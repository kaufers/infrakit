package instance

import (
	"testing"

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
