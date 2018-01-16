package instance

import (
	"fmt"
	"strings"

	"github.com/docker/infrakit/pkg/provider/ibmcloud/client"
	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/softlayer/softlayer-go/filter"
)

const (
	// SoftlayerUsernameEnvVar contains the env var name that the Softlayer terraform
	// provider expects for the Softlayer username
	SoftlayerUsernameEnvVar = "SOFTLAYER_USERNAME"

	// SoftlayerAPIKeyEnvVar contains the env var name that the Softlayer terraform
	// provider expects for the Softlayer API key
	SoftlayerAPIKeyEnvVar = "SOFTLAYER_API_KEY"
)

// mergeLabelsIntoTagSlice combines the tags slice and the labels map into a string slice
// since Softlayer tags are simply strings
func mergeLabelsIntoTagSlice(tags []interface{}, labels map[string]string) []string {
	m := map[string]string{}
	for _, l := range tags {
		line := fmt.Sprintf("%v", l) // conversion using string
		if i := strings.Index(line, ":"); i > 0 {
			key := line[0:i]
			value := ""
			if i+1 < len(line) {
				value = line[i+1:]
			}
			m[key] = value
		} else {
			m[fmt.Sprintf("%v", l)] = ""
		}
	}
	for k, v := range labels {
		m[k] = v
	}

	// now set the final format
	lines := []string{}
	for k, v := range m {
		if v != "" {
			lines = append(lines, fmt.Sprintf("%v:%v", k, v))
		} else {
			lines = append(lines, k)

		}
	}
	return lines
}

// GetIBMCloudInstances returns all VMs that match the optional tag filters
func GetIBMCloudInstances(c *client.SoftlayerClient, tagFilter *string) ([]backend, error) {
	mask := "id,hostname,primaryIpAddress,primaryBackendIpAddress,tagReferences[id,tag[name]]"
	var filters *string
	if tagFilter == nil {
		logger.Info("GetIBMCloudInstances", "msg", "Querying IBM Cloud without any filters")
	} else {
		f := filter.New(filter.Path("virtualGuests.tagReferences.tag.name").Eq(*tagFilter)).Build()
		logger.Info("GetIBMCloudInstances", "msg", fmt.Sprintf("Querying IBM Cloud for VMs with tag filter: %v", f))
		filters = &f
	}
	vms, err := c.GetVirtualGuests(&mask, filters)
	if err != nil {
		return nil, err
	}
	results := []backend{}
	for _, vm := range vms {
		// Needs an ID
		if vm.Id == nil {
			return nil, fmt.Errorf("Returned VM is missing an ID: %v", vm)
		}
		// Map the properties to what we expect to be in the terraform state file
		props := TResourceProperties{}
		if vm.PrimaryIpAddress != nil {
			props["ipv4_address"] = *vm.PrimaryIpAddress
		}
		if vm.PrimaryBackendIpAddress != nil {
			props["ipv4_address_private"] = *vm.PrimaryBackendIpAddress
		}
		if vm.Hostname != nil {
			props["hostname"] = *vm.Hostname
		}
		results = append(
			results,
			backend{
				backendID:    fmt.Sprintf("%v", *vm.Id),
				backendProps: props,
				backendTags:  parseTags(vm),
				complete:     len(props) == 3,
			},
		)
	}
	logger.Info("GetIBMCloudInstances", "backend-size", len(results), "backend-data", results)
	return results, nil
}

// parseTags converts the tag references to the standard map[string]string
func parseTags(vm datatypes.Virtual_Guest) map[string]string {
	tags := map[string]string{}
	for _, ref := range vm.TagReferences {
		tag := *ref.Tag.Name
		if strings.Contains(tag, ":") {
			split := strings.SplitN(tag, ":", 2)
			tags[split[0]] = split[1]
		} else {
			tags[tag] = ""
		}
	}
	return tags
}
