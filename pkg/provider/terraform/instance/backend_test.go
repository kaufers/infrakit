package instance

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetExistingResourcesEmpty(t *testing.T) {
	tf, dir := getPlugin(t)
	defer os.RemoveAll(dir)
	err := tf.getExistingResources([]*backend{})
	require.Error(t, err)
	require.Equal(t, "No backend data provided", err.Error())
}

func TestGetExistingResourceNoVMs(t *testing.T) {
	tf, dir := getPlugin(t)
	defer os.RemoveAll(dir)
	b := backend{
		resType: TResourceType("storage"),
		resName: TResourceName("name"),
	}
	err := tf.getExistingResources([]*backend{&b})
	require.Error(t, err)
	require.Equal(t, "Only VM backend retrieval is supported, invalid resource type: storage", err.Error())
	require.Equal(t, "", b.backendID)
	require.Nil(t, b.backendProps)
	require.False(t, b.complete)
}

func TestGetExistingResourceMultipleVMTypes(t *testing.T) {
	tf, dir := getPlugin(t)
	defer os.RemoveAll(dir)
	b1 := backend{
		resType: VMAmazon,
		resName: TResourceName("name1"),
	}
	b2 := backend{
		resType: VMIBMCloud,
		resName: TResourceName("name2"),
	}
	err := tf.getExistingResources([]*backend{&b1, &b2})
	require.Error(t, err)
	require.Equal(t, "Multiple resource types are not supported, detected both 'aws_instance' and 'ibm_compute_vm_instance'", err.Error())
}

func TestGetExistingResourceUnsupportedType(t *testing.T) {
	tf, dir := getPlugin(t)
	defer os.RemoveAll(dir)

	b := backend{
		resType: VMAzure,
		resName: TResourceName("name"),
	}
	err := tf.getExistingResources([]*backend{&b})
	require.NoError(t, err)
	require.Equal(t, "", b.backendID)
	require.Nil(t, b.backendProps)
	require.False(t, b.complete)
}

func TestGetExistingResourceIBMCloudWrongCreds(t *testing.T) {
	tf, dir := getPlugin(t)
	defer os.RemoveAll(dir)
	// User bogus creds, will always get an error
	tf.envs = []string{
		SoftlayerUsernameEnvVar + "=user",
		SoftlayerAPIKeyEnvVar + "=pass",
	}
	os.Setenv(SoftlayerUsernameEnvVar, "")
	os.Setenv(SoftlayerAPIKeyEnvVar, "")

	b := backend{
		resType:   VMIBMCloud,
		resName:   TResourceName("name"),
		fileProps: TResourceProperties{"tags": []interface{}{"t1", "t2"}},
	}
	err := tf.getExistingResources([]*backend{&b})
	require.Error(t, err)
}

func TestCorrelateInstancesNoPotentialMatches(t *testing.T) {
	b := backend{
		resType:  TResourceType("type1"),
		resName:  TResourceName("name1"),
		fileTags: map[string]string{"t1": "v1"},
	}
	b.correlateInstances([]backend{})
	require.Equal(t,
		backend{
			resType:      TResourceType("type1"),
			resName:      TResourceName("name1"),
			fileTags:     map[string]string{"t1": "v1"},
			fileProps:    nil,
			filename:     "",
			backendID:    "",
			backendProps: nil,
			backendTags:  nil,
			complete:     false,
			description:  nil,
		},
		b)
}

func TestGetUniqueVMByTagsNoMatches(t *testing.T) {
	b := backend{
		resType:  TResourceType("type1"),
		resName:  TResourceName("name1"),
		fileTags: map[string]string{"t1": "v1", "t2": "v2"},
	}
	potentials := []backend{
		{
			backendTags:  map[string]string{"t1": "v1"},
			backendProps: TResourceProperties{"bp1": "val_bp1"},
			backendID:    "id1",
		},
		{
			backendTags:  map[string]string{"t1": "no_match", "t2": "v2"},
			backendProps: TResourceProperties{"bp2": "val_bp2"},
			backendID:    "id2",
		},
		{
			backendTags:  map[string]string{"t1": "v1", "t2": "no_match"},
			backendProps: TResourceProperties{"bp3": "val_bp3"},
			backendID:    "id3",
		},
	}
	b.correlateInstances(potentials)
	require.Equal(t,
		backend{
			resType:      TResourceType("type1"),
			resName:      TResourceName("name1"),
			fileTags:     map[string]string{"t1": "v1", "t2": "v2"},
			fileProps:    nil,
			filename:     "",
			backendID:    "",
			backendProps: nil,
			backendTags:  nil,
			complete:     false,
			description:  nil,
		},
		b)
}

func TestGetUniqueVMByTagsOneMatch(t *testing.T) {
	b := backend{
		resType:  TResourceType("type1"),
		resName:  TResourceName("name1"),
		fileTags: map[string]string{"t1": "v1", "t2": "v2"},
	}
	potentials := []backend{
		{
			backendTags:  map[string]string{"t1": "v1"},
			backendProps: TResourceProperties{"bp1": "val_bp1"},
			backendID:    "id1",
		},
		{
			backendTags:  map[string]string{"t1": "v1", "t2": "v2"},
			backendProps: TResourceProperties{"bp2": "val_bp2"},
			backendID:    "id2",
		},
		{
			backendTags:  map[string]string{"t1": "v1", "t2": "no_match"},
			backendProps: TResourceProperties{"bp3": "val_bp3"},
			backendID:    "id3",
		},
	}
	b.correlateInstances(potentials)
	require.Equal(t,
		backend{
			resType:      TResourceType("type1"),
			resName:      TResourceName("name1"),
			fileTags:     map[string]string{"t1": "v1", "t2": "v2"},
			backendID:    "id2",
			backendProps: TResourceProperties{"bp2": "val_bp2"},
			backendTags:  map[string]string{"t1": "v1", "t2": "v2"},
		},
		b)
}

func TestGetUniqueVMByTagsTwoMatches(t *testing.T) {
	b := backend{
		resType:  TResourceType("type1"),
		resName:  TResourceName("name1"),
		fileTags: map[string]string{"t1": "v1", "t2": "v2"},
	}
	potentials := []backend{
		{
			backendTags:  map[string]string{"t1": "v1"},
			backendProps: TResourceProperties{"bp1": "val_bp1"},
			backendID:    "id1",
		},
		{
			backendTags:  map[string]string{"t1": "v1", "t2": "v2", "t3": "v3-2"},
			backendProps: TResourceProperties{"bp2": "val_bp2"},
			backendID:    "id2",
		},
		{
			backendTags:  map[string]string{"t1": "v1", "t2": "v2", "t3": "v3-3"},
			backendProps: TResourceProperties{"bp3": "val_bp3"},
			backendID:    "id3",
		},
	}
	b.correlateInstances(potentials)
	require.Equal(t,
		backend{
			resType:      TResourceType("type1"),
			resName:      TResourceName("name1"),
			fileTags:     map[string]string{"t1": "v1", "t2": "v2"},
			fileProps:    nil,
			filename:     "",
			backendID:    "",
			backendProps: nil,
			backendTags:  nil,
			complete:     false,
			description:  nil,
		},
		b)
}
