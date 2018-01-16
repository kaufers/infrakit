package instance

import (
	"fmt"
	"os"
	"strings"

	"github.com/deckarep/golang-set"
	ibmcloud_client "github.com/docker/infrakit/pkg/provider/ibmcloud/client"
	"github.com/docker/infrakit/pkg/spi/flavor"
	"github.com/docker/infrakit/pkg/spi/instance"
)

type backend struct {
	resType      TResourceType         // Instance type; parsed from tf.json file
	resName      TResourceName         // Instance name; parsed from tf.json file
	filename     string                // Local filename associated with this instance
	fileProps    TResourceProperties   // Instance properties; parsed from tf.json file
	fileTags     map[string]string     // Instance tags; parsed from tf.json file
	backendProps TResourceProperties   // Backend properites; retrieved from backend
	backendTags  map[string]string     // Backend tags; retrieved from backend
	backendID    string                // Backend instance ID
	complete     bool                  // Denotes if the instance data is complete and does not need to be queried again
	description  *instance.Description // Pointer to an instance.Description for this data
}

// getExistingResource queries the backend cloud to get data associated with the given
// file data
func (p *plugin) getExistingResources(data []*backend) error {
	if len(data) == 0 {
		return fmt.Errorf("No backend data provided")
	}
	supportedVMs := mapset.NewSetFromSlice(VMTypes)
	// Only a single VM is supported
	var resType TResourceType
	for _, d := range data {
		if !supportedVMs.Contains(d.resType) {
			return fmt.Errorf("Only VM backend retrieval is supported, invalid resource type: %s", d.resType)
		}
		if resType == "" {
			resType = d.resType
		} else if resType != d.resType {
			return fmt.Errorf("Multiple resource types are not supported, detected both '%s' and '%s'", resType, d.resType)
		}
	}
	// We only want to make a single backend API call, if we have 1 instance to query then
	// use the unique instance name tag; if we have more then use the swarm ID tag
	var tagName *string
	var tagValue *string
	if len(data) == 1 {
		tags := parseTerraformTags(data[0].resType, data[0].fileProps)
		data[0].fileTags = tags
		tag := NameTag
		if nameVal, has := tags[tag]; has {
			tagName = &tag
			tagValue = &nameVal
		} else {
			logger.Warn("getExistingResources",
				"msg", "Single instance missing name tag, querying without filter",
				"tags", tags)
		}
	} else {
		// Verify that we have the same value for all swarm IDs
		var clusterID string
		includeFilter := true
		tag := flavor.ClusterIDTag
		for _, d := range data {
			tags := parseTerraformTags(d.resType, d.fileProps)
			d.fileTags = tags
			if val, has := tags[tag]; has {
				if clusterID == "" {
					clusterID = val
				} else if clusterID != val {
					logger.Warn("getExistingResources",
						"msg", "Multiple cluster ID values, detected both '%s' and '%s'", clusterID, val)
					includeFilter = false
				}
			} else {
				logger.Warn("getExistingResources",
					"msg", "Single instance missing \"swarm-id\" tag, querying without filter",
					"tags", tags)
				includeFilter = false
			}
		}
		if includeFilter {
			tagName = &tag
			tagValue = &clusterID
		}
	}
	// All matching backend VMs, this will be correladed with the given data using server-side tag filtering
	backendData := []backend{}

	switch resType {
	case VMSoftLayer, VMIBMCloud:
		// Creds either in env vars or in the plugin Env slice
		username := os.Getenv(SoftlayerUsernameEnvVar)
		apiKey := os.Getenv(SoftlayerAPIKeyEnvVar)
		if username == "" || apiKey == "" {
			for _, env := range p.envs {
				if !strings.Contains(env, "=") {
					continue
				}
				split := strings.Split(env, "=")
				switch split[0] {
				case SoftlayerUsernameEnvVar:
					username = split[1]
				case SoftlayerAPIKeyEnvVar:
					apiKey = split[1]
				}
			}
		}
		var tagFilter *string
		if tagName != nil && tagValue != nil {
			filter := fmt.Sprintf("%s:%s", *tagName, *tagValue)
			tagFilter = &filter
		}
		vms, err := GetIBMCloudInstances(ibmcloud_client.GetClient(username, apiKey), tagFilter)
		if err != nil {
			return err
		}
		backendData = vms
	default:
		logger.Warn("getExistingResource", "msg", fmt.Sprintf("Unsupported VM type for backend retrival: %v", resType))
	}
	// Associate the tag filtered VMs to the file data
	for _, b := range data {
		b.correlateInstances(backendData)
	}
	return nil
}

// correlateInstances populates the backend ID, properties, and tags of the receiver if exactly
// one backend instance matches the reciever's tags
func (b *backend) correlateInstances(potentialMatches []backend) {
	if len(potentialMatches) == 0 {
		return
	}
	matches := []backend{}
	// All file tags must match the backend tags
	for _, p := range potentialMatches {
		allTagsMatch := true
		for tagKey, tagVal := range b.fileTags {
			tagMatch := false
			for backendTagKey, backendTagVal := range p.backendTags {
				if tagKey != backendTagKey {
					continue
				}
				if tagVal != backendTagVal {
					continue
				}
				tagMatch = true
				break
			}
			if !tagMatch {
				allTagsMatch = false
				break
			}
		}
		if allTagsMatch {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		logger.Info("correlateInstances",
			"msg", fmt.Sprintf("Detected 0 existing VMs with tags: %v", b.fileTags))
		return
	}
	// Exactly 1 match
	if len(matches) == 1 {
		logger.Info("correlateInstances",
			"msg", fmt.Sprintf("Existing VM %v with ID %v matches tags: %v", b.resName, matches[0].backendID, b.fileTags))
		b.backendID = matches[0].backendID
		b.backendProps = matches[0].backendProps
		b.backendTags = matches[0].backendTags
		return
	}
	// More than 1 match
	ids := []string{}
	for _, match := range matches {
		ids = append(ids, match.backendID)
	}
	logger.Error("correlateInstances",
		"msg", fmt.Sprintf("Only a single VM should match tags, but VMs %v match tags: %v", ids, b.fileTags))
}
