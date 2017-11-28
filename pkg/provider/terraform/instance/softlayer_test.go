package instance

import (
	"testing"

	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/stretchr/testify/require"
)

func TestMergeLabelsIntoTagSliceEmpty(t *testing.T) {
	result := mergeLabelsIntoTagSlice([]interface{}{}, map[string]string{})
	require.Equal(t, []string{}, result)
}

func TestMergeLabelsIntoTagSliceTagsOnly(t *testing.T) {
	result := mergeLabelsIntoTagSlice(
		[]interface{}{
			"tag1:val1",
			"tag2:val2",
		},
		map[string]string{},
	)
	require.Len(t, result, 2)
	require.Contains(t, result, "tag1:val1")
	require.Contains(t, result, "tag2:val2")
}

func TestMergeLabelsIntoTagSliceLabelsOnly(t *testing.T) {
	result := mergeLabelsIntoTagSlice(
		[]interface{}{},
		map[string]string{
			"label1": "val1",
			"label2": "val2",
		},
	)
	require.Len(t, result, 2)
	require.Contains(t, result, "label1:val1")
	require.Contains(t, result, "label2:val2")
}

func TestMergeLabelsIntoTagSlice(t *testing.T) {
	result := mergeLabelsIntoTagSlice(
		[]interface{}{
			"tag1:val1",
			"tag2:val2",
		},
		map[string]string{
			"label1": "val1",
			"label2": "val2",
		},
	)
	require.Len(t, result, 4)
	require.Contains(t, result, "tag1:val1")
	require.Contains(t, result, "tag2:val2")
	require.Contains(t, result, "label1:val1")
	require.Contains(t, result, "label2:val2")
}

func TestFilterVMsByTagsEmpty(t *testing.T) {
	vms := []datatypes.Virtual_Guest{}
	filterVMsByTags(&vms, []string{})
	require.Equal(t, []datatypes.Virtual_Guest{}, vms)
}

// getVMs is a utility function to get datatypes.Virtual_Guest with tags
func getVMs() []datatypes.Virtual_Guest {
	vmID0 := 0
	vmID1 := 1
	vmID2 := 2
	vmID3 := 3
	tag1Name := "tag1"
	tag1 := datatypes.Tag{Name: &tag1Name}
	tag2Name := "tag2"
	tag2 := datatypes.Tag{Name: &tag2Name}
	tag3Name := "tag3"
	tag3 := datatypes.Tag{Name: &tag3Name}
	vms := []datatypes.Virtual_Guest{
		{
			TagReferences: []datatypes.Tag_Reference{},
			Id:            &vmID0,
		},
		{
			TagReferences: []datatypes.Tag_Reference{{Tag: &tag1}},
			Id:            &vmID1,
		},
		{
			TagReferences: []datatypes.Tag_Reference{{Tag: &tag1}, {Tag: &tag2}},
			Id:            &vmID2,
		},
		{
			TagReferences: []datatypes.Tag_Reference{{Tag: &tag1}, {Tag: &tag2}, {Tag: &tag3}},
			Id:            &vmID3,
		},
	}
	return vms
}

func TestFilterVMsByTags(t *testing.T) {
	// No tags given, everything matches
	vms := getVMs()
	filterVMsByTags(&vms, []string{})
	require.Len(t, vms, 4)
	require.Equal(t, 0, *vms[0].Id)
	require.Equal(t, 1, *vms[1].Id)
	require.Equal(t, 2, *vms[2].Id)
	require.Equal(t, 3, *vms[3].Id)
	// Empty tag, nothing matches
	vms = getVMs()
	filterVMsByTags(&vms, []string{""})
	require.Len(t, vms, 0)
	// 1 tag matches
	vms = getVMs()
	filterVMsByTags(&vms, []string{"tag1"})
	require.Len(t, vms, 3)
	require.Equal(t, 1, *vms[0].Id)
	require.Equal(t, 2, *vms[1].Id)
	require.Equal(t, 3, *vms[2].Id)
	// 2 tags match
	vms = getVMs()
	filterVMsByTags(&vms, []string{"tag1", "tag2"})
	require.Len(t, vms, 2)
	require.Equal(t, 2, *vms[0].Id)
	require.Equal(t, 3, *vms[1].Id)
	// 3 tags match
	vms = getVMs()
	filterVMsByTags(&vms, []string{"tag1", "tag2", "tag3"})
	require.Len(t, vms, 1)
	require.Equal(t, 3, *vms[0].Id)
	// A tag that doesn't match
	vms = getVMs()
	filterVMsByTags(&vms, []string{"tag1", "foo"})
	require.Len(t, vms, 0)
}
