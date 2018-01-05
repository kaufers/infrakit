package enrollment

import (
	"fmt"
	"net/url"
	"testing"

	enrollment "github.com/docker/infrakit/pkg/controller/enrollment/types"
	"github.com/docker/infrakit/pkg/discovery"
	"github.com/docker/infrakit/pkg/plugin"
	"github.com/docker/infrakit/pkg/run/scope"
	"github.com/docker/infrakit/pkg/spi/group"
	"github.com/docker/infrakit/pkg/spi/instance"
	"github.com/docker/infrakit/pkg/spi/stack"
	group_test "github.com/docker/infrakit/pkg/testing/group"
	instance_test "github.com/docker/infrakit/pkg/testing/instance"
	"github.com/docker/infrakit/pkg/types"
	"github.com/stretchr/testify/require"
)

func fakeLeader(v bool) func() stack.Leadership {
	return func() stack.Leadership { return fakeLeaderT(v) }
}

type fakeLeaderT bool

func (f fakeLeaderT) IsLeader() (bool, error) {
	return bool(f), nil
}

func (f fakeLeaderT) LeaderLocation() (*url.URL, error) {
	return nil, nil
}

type fakePlugins map[string]*plugin.Endpoint

func (f fakePlugins) Find(name plugin.Name) (*plugin.Endpoint, error) {
	lookup, _ := name.GetLookupAndType()
	if v, has := f[lookup]; has {
		return v, nil
	}
	return nil, fmt.Errorf("not found")
}

func (f fakePlugins) List() (map[string]*plugin.Endpoint, error) {
	return (map[string]*plugin.Endpoint)(f), nil
}

func TestEnrollerOptions(t *testing.T) {
	// Verify defaults
	e := newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{})
	require.Equal(t, enrollment.SourceParseErrorEnableDestroy, e.options.SourceParseErrOp)
	require.Equal(t, enrollment.EnrolledParseErrorEnableProvision, e.options.EnrollmentParseErrOp)
	// Override error options
	e = newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{
			SourceParseErrOp:     enrollment.SourceParseErrorDisableDestroy,
			EnrollmentParseErrOp: enrollment.EnrolledParseErrorDisableProvision,
		})
	require.Equal(t, enrollment.SourceParseErrorDisableDestroy, e.options.SourceParseErrOp)
	require.Equal(t, enrollment.EnrolledParseErrorDisableProvision, e.options.EnrollmentParseErrOp)
	// Invalid values, should change to defaults
	e = newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{
			SourceParseErrOp:     "bogus-SourceParseErrOp",
			EnrollmentParseErrOp: "bogus-EnrollmentParseErrOp",
		})
	require.Equal(t, enrollment.SourceParseErrorEnableDestroy, e.options.SourceParseErrOp)
	require.Equal(t, enrollment.EnrolledParseErrorEnableProvision, e.options.EnrollmentParseErrOp)
}

func TestEnroller(t *testing.T) {

	source := []instance.Description{
		{ID: instance.ID("h1")},
		{ID: instance.ID("h2")},
		{ID: instance.ID("h3")},
	}

	enrolled := []instance.Description{
		{ID: instance.ID("nfs1"), Tags: map[string]string{"infrakit.enrollment.sourceID": "h1"}},
		{ID: instance.ID("nfs2"), Tags: map[string]string{"infrakit.enrollment.sourceID": "h2"}},
		{ID: instance.ID("nfs5"), Tags: map[string]string{"infrakit.enrollment.sourceID": "h5"}},
	}

	seen := make(chan []interface{}, 10)

	enroller := newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{})
	enroller.groupPlugin = &group_test.Plugin{
		DoDescribeGroup: func(gid group.ID) (group.Description, error) {
			result := group.Description{Instances: source}
			return result, nil
		},
	}
	enroller.instancePlugin = &instance_test.Plugin{
		DoDescribeInstances: func(t map[string]string, p bool) ([]instance.Description, error) {
			return enrolled, nil
		},
		DoProvision: func(spec instance.Spec) (*instance.ID, error) {

			seen <- []interface{}{spec, "Provision"}
			return nil, nil
		},
		DoDestroy: func(id instance.ID, ctx instance.Context) error {

			seen <- []interface{}{id, ctx, "Destroy"}
			return nil
		},
	}

	require.False(t, enroller.Running())

	spec := types.Spec{}
	require.NoError(t, types.AnyYAMLMust([]byte(`
kind: enrollment
metadata:
  name: nfs
properties:
  List: group/workers
  Instance:
    Plugin: nfs/authorization
    Properties:
       host: \{\{.ID\}\}
       iops: 10
options:
  SourceKeySelector: \{\{.ID\}\}

`)).Decode(&spec))

	require.NoError(t, enroller.updateSpec(spec))

	st, err := enroller.getSourceKeySelectorTemplate()
	require.NoError(t, err)
	require.NotNil(t, st)

	et, err := enroller.getEnrollmentPropertiesTemplate()
	require.NoError(t, err)
	require.NotNil(t, et)

	// Should use the defaults
	require.Equal(t, enrollment.SourceParseErrorEnableDestroy, enroller.options.SourceParseErrOp)
	require.Equal(t, enrollment.EnrolledParseErrorEnableProvision, enroller.options.EnrollmentParseErrOp)

	require.NoError(t, err)

	s, err := enroller.getSourceInstances()
	require.NoError(t, err)
	require.Equal(t, source, s)

	found, err := enroller.getEnrolledInstances()
	require.NoError(t, err)
	require.Equal(t, enrolled, found)

	require.NoError(t, enroller.sync())

	// check the provision and destroy calls
	require.Equal(t, []interface{}{
		instance.Spec{
			Properties: types.AnyString(`{"host":"h3","iops":10}`),
			Tags: map[string]string{
				"infrakit.enrollment.sourceID": "h3",
				"infrakit.enrollment.name":     "nfs",
			},
		},
		"Provision",
	}, <-seen)
	require.Equal(t, []interface{}{
		instance.ID("nfs5"),
		instance.Termination,
		"Destroy",
	}, <-seen)
}

func TestEnrollerNoTags(t *testing.T) {

	// Group members: 1, 2, 3
	source := []instance.Description{
		{ID: instance.ID("instance-1"), Properties: types.AnyString(`{"backend_id":"1"}`)},
		{ID: instance.ID("instance-2"), Properties: types.AnyString(`{"backend_id":"2"}`)},
		{ID: instance.ID("instance-3"), Properties: types.AnyString(`{"backend_id":"3"}`)},
	}

	// Currently enrolled: 1, 2, 4
	enrolled := []instance.Description{
		{ID: instance.ID("1")},
		{ID: instance.ID("2")},
		{ID: instance.ID("4")},
	}

	seen := make(chan []interface{}, 10)

	enroller := newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{})
	enroller.groupPlugin = &group_test.Plugin{
		DoDescribeGroup: func(gid group.ID) (group.Description, error) {
			result := group.Description{Instances: source}
			return result, nil
		},
	}
	enroller.instancePlugin = &instance_test.Plugin{
		DoDescribeInstances: func(t map[string]string, p bool) ([]instance.Description, error) {
			return enrolled, nil
		},
		DoProvision: func(spec instance.Spec) (*instance.ID, error) {

			seen <- []interface{}{spec, "Provision"}
			return nil, nil
		},
		DoDestroy: func(id instance.ID, ctx instance.Context) error {

			seen <- []interface{}{id, ctx, "Destroy"}
			return nil
		},
	}

	require.False(t, enroller.Running())

	// Build a spec that uses the "backend_id" as the key for the source and just
	// the "ID" for the enrolled
	spec := types.Spec{}
	require.NoError(t, types.AnyYAMLMust([]byte(`
kind: enrollment
metadata:
  name: nfs
properties:
  List: group/workers
  Instance:
    Plugin: nfs/authorization
    Properties:
       backend_id: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.backend_id \}\}
options:
  SourceKeySelector: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.backend_id \}\}
  EnrollmentKeySelector: \{\{.ID\}\}
`)).Decode(&spec))

	require.NoError(t, enroller.updateSpec(spec))

	st, err := enroller.getSourceKeySelectorTemplate()
	require.NoError(t, err)
	require.NotNil(t, st)

	et, err := enroller.getEnrollmentPropertiesTemplate()
	require.NoError(t, err)
	require.NotNil(t, et)

	require.NoError(t, err)

	s, err := enroller.getSourceInstances()
	require.NoError(t, err)
	require.Equal(t, source, s)

	found, err := enroller.getEnrolledInstances()
	require.NoError(t, err)
	require.Equal(t, enrolled, found)

	require.NoError(t, enroller.sync())

	// check the provision and destroy calls, instance 3 should be added and 4
	// should be removed
	require.Equal(t, []interface{}{
		instance.Spec{
			Properties: types.AnyString(`{"backend_id":"3"}`),
			Tags: map[string]string{
				"infrakit.enrollment.sourceID": "instance-3",
				"infrakit.enrollment.name":     "nfs",
			},
		},
		"Provision",
	}, <-seen)
	require.Equal(t, []interface{}{
		instance.ID("4"),
		instance.Termination,
		"Destroy",
	}, <-seen)
	require.Len(t, seen, 0)
}

func TestEnrollerSourceParseError(t *testing.T) {

	// Group members: 1, 2 (no props), 3 (empty props)
	source := []instance.Description{
		{ID: instance.ID("instance-1"), Properties: types.AnyString(`{"backend_id":"1"}`)},
		{ID: instance.ID("instance-2")},
		{ID: instance.ID("instance-3"), Properties: types.AnyString(`{}`)},
	}

	// Currently enrolled. Missing 1 (should be added). 2/3/4 should be removed only if
	// source parsing errors are ignored.
	enrolled := []instance.Description{
		{ID: instance.ID("2")},
		{ID: instance.ID("3")},
		{ID: instance.ID("4")},
	}

	seenProvision := make(chan []interface{}, 10)
	seenDestroy := make(chan []interface{}, 10)

	enroller := newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{})
	enroller.groupPlugin = &group_test.Plugin{
		DoDescribeGroup: func(gid group.ID) (group.Description, error) {
			result := group.Description{Instances: source}
			return result, nil
		},
	}
	enroller.instancePlugin = &instance_test.Plugin{
		DoDescribeInstances: func(t map[string]string, p bool) ([]instance.Description, error) {
			return enrolled, nil
		},
		DoProvision: func(spec instance.Spec) (*instance.ID, error) {

			seenProvision <- []interface{}{spec, "Provision"}
			return nil, nil
		},
		DoDestroy: func(id instance.ID, ctx instance.Context) error {

			seenDestroy <- []interface{}{id, ctx, "Destroy"}
			return nil
		},
	}

	require.False(t, enroller.Running())

	// Verify the various options for the SourceParseError
	for _, srcParseError := range []string{"", "foo",
		enrollment.SourceParseErrorDisableDestroy,
		enrollment.SourceParseErrorEnableDestroy} {

		// Build a spec that uses the "backend_id" as the key for the source and just
		// the "ID" for the enrolled
		spec := types.Spec{}
		require.NoError(t, types.AnyYAMLMust([]byte(fmt.Sprintf(`
kind: enrollment
metadata:
  name: nfs
properties:
  List: group/workers
  Instance:
    Plugin: nfs/authorization
    Properties:
       backend_id: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.backend_id \}\}
options:
  SourceKeySelector: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.backend_id \}\}
  SourceParseErrOp: %s
  EnrollmentKeySelector: \{\{.ID\}\}
`, srcParseError))).Decode(&spec))

		require.NoError(t, enroller.updateSpec(spec))

		st, err := enroller.getSourceKeySelectorTemplate()
		require.NoError(t, err)
		require.NotNil(t, st)

		et, err := enroller.getEnrollmentPropertiesTemplate()
		require.NoError(t, err)
		require.NotNil(t, et)

		s, err := enroller.getSourceInstances()
		require.NoError(t, err)
		require.Equal(t, source, s)

		found, err := enroller.getEnrolledInstances()
		require.NoError(t, err)
		require.Equal(t, enrolled, found)

		// Sync the enroller
		require.NoError(t, enroller.sync())

		// Verify the destroy, which is dependent on the source parse error option
		if srcParseError == enrollment.SourceParseErrorDisableDestroy {
			// Not enabling destroy, should always be 0
			require.Len(t,
				seenDestroy, 0,
				fmt.Sprintf("seenDestroy length should be 0, actual is %v, srcParseError is '%s'", len(seenDestroy), srcParseError))
		} else {
			// Enabling the destroy, should be 3 since parsing errors are ignored
			require.Len(t,
				seenDestroy, 3,
				fmt.Sprintf("seenDestroy length should be 3, actual is %v, srcParseError is '%s'", len(seenDestroy), srcParseError))
			for _, id := range []string{"2", "3", "4"} {
				require.Equal(t, []interface{}{
					instance.ID(id),
					instance.Context{Reason: "terminate"},
					"Destroy",
				}, <-seenDestroy)
			}
			require.Len(t, seenDestroy, 0)
		}

		// Provision is constant since there are no enrolled parsing errors, 1 should always be added
		require.Len(t,
			seenProvision, 1,
			fmt.Sprintf("seenProvision length should be 3, actual is %v, srcParseError is '%s'", len(seenProvision), srcParseError))
		require.Equal(t, []interface{}{
			instance.Spec{
				Properties: types.AnyString(`{"backend_id":"1"}`),
				Tags: map[string]string{
					"infrakit.enrollment.sourceID": "instance-1",
					"infrakit.enrollment.name":     "nfs",
				},
			},
			"Provision",
		}, <-seenProvision)
		require.Len(t, seenProvision, 0)
	}
}

func TestEnrollerEnrolledParseError(t *testing.T) {

	// Group members: 1, 2, 3
	source := []instance.Description{
		{ID: instance.ID("instance-1"), Properties: types.AnyString(`{"backend_id":"1"}`)},
		{ID: instance.ID("instance-2"), Properties: types.AnyString(`{"backend_id":"2"}`)},
		{ID: instance.ID("instance-3"), Properties: types.AnyString(`{"backend_id":"3"}`)},
	}

	// Currently enrolled. 1 is enrolled. 2/3 should be added only if parsing errors are ignored and 4
	// should always be removed.
	enrolled := []instance.Description{
		{ID: instance.ID("instance-1"), Properties: types.AnyString(`{"ID":"1"}`)},
		{ID: instance.ID("instance-2"), Properties: types.AnyString("{}")},
		{ID: instance.ID("instance-3")},
		{ID: instance.ID("instance-4"), Properties: types.AnyString(`{"ID":"4"}`)},
	}

	seenProvision := make(chan []interface{}, 10)
	seenDestroy := make(chan []interface{}, 10)

	enroller := newEnroller(
		scope.DefaultScope(func() discovery.Plugins {
			return fakePlugins{
				"test": &plugin.Endpoint{},
			}
		}),
		fakeLeader(false),
		enrollment.Options{})
	enroller.groupPlugin = &group_test.Plugin{
		DoDescribeGroup: func(gid group.ID) (group.Description, error) {
			result := group.Description{Instances: source}
			return result, nil
		},
	}
	enroller.instancePlugin = &instance_test.Plugin{
		DoDescribeInstances: func(t map[string]string, p bool) ([]instance.Description, error) {
			return enrolled, nil
		},
		DoProvision: func(spec instance.Spec) (*instance.ID, error) {

			seenProvision <- []interface{}{spec, "Provision"}
			return nil, nil
		},
		DoDestroy: func(id instance.ID, ctx instance.Context) error {

			seenDestroy <- []interface{}{id, ctx, "Destroy"}
			return nil
		},
	}

	require.False(t, enroller.Running())

	// Verify the various options for the EnrollmentParseErrOp
	for _, enrolledParseError := range []string{"", "foo",
		enrollment.EnrolledParseErrorEnableProvision,
		enrollment.EnrolledParseErrorDisableProvision} {

		// Build a spec that uses the "backend_id" as the key for the source and just
		// the "ID" for the enrolled
		spec := types.Spec{}
		require.NoError(t, types.AnyYAMLMust([]byte(fmt.Sprintf(`
kind: enrollment
metadata:
  name: nfs
properties:
  List: group/workers
  Instance:
    Plugin: nfs/authorization
    Properties:
       backend_id: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.backend_id \}\}
options:
  SourceKeySelector: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.backend_id \}\}
  EnrollmentKeySelector: \{\{ $x := .Properties | jsonDecode \}\}\{\{ int $x.ID \}\}
  EnrollmentParseErrOp: %s
`, enrolledParseError))).Decode(&spec))

		require.NoError(t, enroller.updateSpec(spec))

		st, err := enroller.getSourceKeySelectorTemplate()
		require.NoError(t, err)
		require.NotNil(t, st)

		et, err := enroller.getEnrollmentPropertiesTemplate()
		require.NoError(t, err)
		require.NotNil(t, et)

		s, err := enroller.getSourceInstances()
		require.NoError(t, err)
		require.Equal(t, source, s)

		found, err := enroller.getEnrolledInstances()
		require.NoError(t, err)
		require.Equal(t, enrolled, found)

		// Sync the enroller
		require.NoError(t, enroller.sync())

		// Verify the provision, which is dependent on the enrolled parse error option
		if enrolledParseError == enrollment.EnrolledParseErrorDisableProvision {
			// Not enabling provision, should always be 0
			require.Len(t,
				seenProvision, 0,
				fmt.Sprintf("seenProvision length should be 0, actual is %v, enrolledParseError is '%s'", len(seenProvision), enrolledParseError))
		} else {
			// Enabling the Provision, should be 2 since parsing errors are ignored
			require.Len(t,
				seenProvision, 2,
				fmt.Sprintf("seenProvision length should be 3, actual is %v, enrolledParseError is '%s'", len(seenProvision), enrolledParseError))
			for _, id := range []string{"2", "3"} {
				require.Equal(t, []interface{}{
					instance.Spec{
						Properties: types.AnyString(fmt.Sprintf(`{"backend_id":"%s"}`, id)),
						Tags: map[string]string{
							"infrakit.enrollment.sourceID": fmt.Sprintf("instance-%s", id),
							"infrakit.enrollment.name":     "nfs",
						},
					},
					"Provision",
				}, <-seenProvision)
			}
			require.Len(t, seenProvision, 0)
		}

		// Destroy is constant since there are no source parsing errors, 1 should always be removed
		// require.Len(t,
		// 	seenDestroy, 1,
		// 	fmt.Sprintf("seenDestroy length should be 1, actual is %v, enrolledParseError is '%s'", len(seenDestroy), enrolledParseError))
		// require.Equal(t, []interface{}{
		// 	instance.ID("4"),
		// 	instance.Context{Reason: "terminate"},
		// 	"Destroy",
		// }, <-seenDestroy)
		// require.Len(t, seenDestroy, 0)
	}
}
