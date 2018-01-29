package swarm

import (
	"fmt"

	docker_types "github.com/docker/docker/api/types"
	"github.com/docker/infrakit/pkg/run/scope"
	"github.com/docker/infrakit/pkg/spi/group"
	"github.com/docker/infrakit/pkg/spi/instance"
	"github.com/docker/infrakit/pkg/types"
	"github.com/docker/infrakit/pkg/util/docker"
	"golang.org/x/net/context"
)

// NewGroupFlavor creates the struct for the Group flavor
func NewGroupFlavor(scope scope.Scope,
	connectFn func(Spec) (docker.APIClientCloser, error),
	connectInfo docker.ConnectInfo) *GroupFlavor {
	base := &baseFlavor{
		getDockerClient: connectFn,
		scope:           scope,
	}
	return &GroupFlavor{
		base:        base,
		connectinfo: connectInfo,
	}
}

// GroupFlavor is the flavor for the swarm node group
type GroupFlavor struct {
	base        *baseFlavor
	connectinfo docker.ConnectInfo
}

// DescribeGroup .
func (s *GroupFlavor) DescribeGroup(group.ID) (group.Description, error) {
	dockerClient, err := s.base.getDockerClient(Spec{Docker: s.connectinfo})
	if err != nil {
		return group.Description{}, err
	}
	defer dockerClient.Close()

	nodes, err := dockerClient.NodeList(context.Background(), docker_types.NodeListOptions{})
	if err != nil {
		return group.Description{}, err
	}
	result := group.Description{}
	for _, n := range nodes {
		props := map[string]interface{}{
			"Spec":          n.Spec,
			"Status":        n.Status,
			"Meta":          n.Meta,
			"Description":   n.Description,
			"ManagerStatus": n.ManagerStatus,
		}
		propsAny, err := types.AnyValue(props)
		if err != nil {
			log.Error("DescribeGroup", "msg", "Failed to encode node properties", "error", err)
			return group.Description{}, err
		}
		d := instance.Description{
			ID:         instance.ID(n.ID),
			Properties: propsAny,
		}
		result.Instances = append(result.Instances, d)
	}
	return result, nil
}

// Size .
func (s *GroupFlavor) Size(group.ID) (int, error) {
	dockerClient, err := s.base.getDockerClient(Spec{Docker: s.connectinfo})
	if err != nil {
		return 0, err
	}
	defer dockerClient.Close()

	nodes, err := dockerClient.NodeList(context.Background(), docker_types.NodeListOptions{})
	if err != nil {
		return 0, err
	}
	return len(nodes), nil
}

// CommitGroup is not suported
func (s *GroupFlavor) CommitGroup(grp group.Spec, pretend bool) (string, error) {
	return "", fmt.Errorf("CommitGroup not supported for swarm group")
}

// FreeGroup is not suported
func (s *GroupFlavor) FreeGroup(group.ID) error {
	return fmt.Errorf("FreeGroup not supported for swarm group")
}

// InspectGroups is not suported
func (s *GroupFlavor) InspectGroups() ([]group.Spec, error) {
	return []group.Spec{}, fmt.Errorf("InspectGroups not supported for swarm group")
}

// DestroyGroup is not suported
func (s *GroupFlavor) DestroyGroup(group.ID) error {
	return fmt.Errorf("DestroyGroup not supported for swarm group")
}

// DestroyInstances is not suported
func (s *GroupFlavor) DestroyInstances(group.ID, []instance.ID) error {
	return fmt.Errorf("DestroyInstances not supported for swarm group")
}

// SetSize is not suported
func (s *GroupFlavor) SetSize(group.ID, int) error {
	return fmt.Errorf("SetSize not supported for swarm group")
}
