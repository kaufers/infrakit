package swarm

import (
	"errors"
	"fmt"
	"time"

	docker_types "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/infrakit/pkg/plugin/metadata"
	"github.com/docker/infrakit/pkg/run/scope"
	"github.com/docker/infrakit/pkg/spi/group"
	"github.com/docker/infrakit/pkg/spi/instance"
	"github.com/docker/infrakit/pkg/template"
	"github.com/docker/infrakit/pkg/types"
	"github.com/docker/infrakit/pkg/util/docker"
	"golang.org/x/net/context"
)

// NewManagerFlavor creates a flavor.Plugin that creates manager and worker nodes connected in a swarm.
func NewManagerFlavor(scope scope.Scope, connect func(Spec) (docker.APIClientCloser, error),
	templ *template.Template,
	stop <-chan struct{}, self *instance.LogicalID) *ManagerFlavor {

	base := &baseFlavor{initScript: templ, getDockerClient: connect, scope: scope, self: self}
	base.metadataPlugin = metadata.NewPluginFromChannel(base.runMetadataSnapshot(stop))
	return &ManagerFlavor{baseFlavor: base}
}

// ManagerFlavor is the flavor for swarm managers
type ManagerFlavor struct {
	*baseFlavor
}

// Validate checks whether the helper can support a configuration.
func (s *ManagerFlavor) Validate(flavorProperties *types.Any, allocation group.AllocationMethod) error {

	if err := s.baseFlavor.Validate(flavorProperties, allocation); err != nil {
		return err
	}

	spec := Spec{}
	err := flavorProperties.Decode(&spec)
	if err != nil {
		return err
	}

	if len(allocation.LogicalIDs)%2 == 0 {
		return errors.New("must have odd number for quorum")
	}

	for _, id := range allocation.LogicalIDs {
		if att, exists := spec.Attachments[id]; !exists || len(att) == 0 {
			log.Warn("No attachments, which is needed for durability", "id", id)
		}
	}
	return nil
}

// Prepare sets up the provisioner / instance plugin's spec based on information about the swarm to join.
func (s *ManagerFlavor) Prepare(flavorProperties *types.Any,
	instanceSpec instance.Spec, allocation group.AllocationMethod,
	index group.Index) (instance.Spec, error) {
	return s.baseFlavor.prepare("manager", flavorProperties, instanceSpec, allocation, index)
}

// Drain in the case of manager, first perform a swarm node demote to
// downgrade the manager to a worker, then do a swarm leave.
// Note that if the current node is the leader running this code, the demote
// will turn the manager to a worker, and it's not possible to issue a
// docker node rm anymore because this node is no longer a manager and only
// manager nodes permit `docker node rm`.  So the node demote will be followed
// by `docker swarm leave` of *this* node.  This in essence takes the current
// leader node out of the swarm.
func (s *ManagerFlavor) Drain(flavorProperties *types.Any, inst instance.Description) error {
	if flavorProperties == nil {
		return fmt.Errorf("missing config")
	}

	spec := Spec{}
	err := flavorProperties.Decode(&spec)
	if err != nil {
		return err
	}

	link := types.NewLinkFromMap(inst.Tags)
	if !link.Valid() {
		return fmt.Errorf("Unable to drain %s without an association tag", inst.ID)
	}

	filter := filters.NewArgs()
	filter.Add("label", fmt.Sprintf("%s=%s", link.Label(), link.Value()))

	dockerClient, err := s.getDockerClient(spec)
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	ctx := context.Background()

	nodes, err := dockerClient.NodeList(ctx, docker_types.NodeListOptions{Filters: filter})
	if err != nil {
		return err
	}

	switch {
	case len(nodes) == 0:
		log.Warn("Unable to drain - not found in swarm", "id", inst.ID)
		return nil

	case len(nodes) == 1:

		// Do a swarm leave if and only if this is a manager

		nodeID := nodes[0].ID
		// read the state of the node, getting the current version
		nodeInfo, _, err := dockerClient.NodeInspectWithRaw(ctx, nodeID)
		if err != nil {
			return err
		}
		version := nodeInfo.Version

		// If the node is not a manager then remove it from the swarm (it is possible that the node
		// was demoted but then failed to be removed and Destroyed)
		if nodeInfo.Spec.Role != swarm.NodeRoleManager {
			log.Warn("Node is not a manager, attempting to remove it", "hostname", nodeInfo.Description.Hostname, "id", nodeID)
			err := dockerClient.NodeRemove(
				ctx,
				nodeID,
				docker_types.NodeRemoveOptions{Force: true})
			if err != nil {
				return err
			}
			return nil
		}

		// change to worker
		nodeInfo.Spec.Role = swarm.NodeRoleWorker

		log.Info("Docker NodeDemote", "hostname", nodeInfo.Description.Hostname, "id", nodeID)
		err = dockerClient.NodeUpdate(
			ctx,
			nodeID,
			version,
			nodeInfo.Spec)
		if err != nil {
			return err
		}

		go func() {
			// Wait a few seconds and then verify that the node is now a worker
			attempt := 0
			maxAttempts := 11
			for {
				attempt++
				time.Sleep(time.Duration(10) * time.Second)
				// read the state of the node, getting the current version
				nodeInfo, _, err := dockerClient.NodeInspectWithRaw(ctx, nodeID)
				if err == nil {
					if nodeInfo.Spec.Role == swarm.NodeRoleWorker {
						log.Info("Node has been demoted", "hostname", nodeInfo.Description.Hostname, "id", nodeID)
						break
					}
					log.Warn("Node has not yet demoted to worker",
						"role", nodeInfo.Spec.Role,
						"hostname", nodeInfo.Description.Hostname,
						"id", nodeID,
						"error", err)
					err = fmt.Errorf("Node failed to demote")
				} else {
					log.Error("Failed to inspect node after demote",
						"hostname", nodeInfo.Description.Hostname,
						"id", nodeID,
						"error", err)
				}
				if attempt < maxAttempts {
					continue
				}
				return
			}

			// If running on the same node (self), then do docker swarm leave
			// otherwise, remove the node
			if s.isSelf(inst) {
				log.Info("Docker SwarmLeave", "hostname", nodeInfo.Description.Hostname, "id", nodeID)
				err := dockerClient.SwarmLeave(ctx, true)
				if err != nil {
					log.Info("Failed to leave swarm", "hostname", nodeInfo.Description.Hostname, "id", nodeID, "error", err)
				}
			} else {
				log.Info("Docker NodeRemote", "hostname", nodeInfo.Description.Hostname, "id", nodeID)
				err := dockerClient.NodeRemove(
					ctx,
					nodeID,
					docker_types.NodeRemoveOptions{Force: true})
				if err != nil {
					log.Info("Failed to remove node from swarm", "hostname", nodeInfo.Description.Hostname, "id", nodeID, "error", err)
				}
			}
		}()

		return nil

	default:
		return fmt.Errorf("Expected at most one node with label %s, but found %v", link.Value(), nodes)
	}
}
