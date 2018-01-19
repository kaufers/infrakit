package instance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/softlayer/softlayer-go/filter"
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

type FakeSoftlayer struct {
	GetVirtualGuestsStub func(mask, filters *string) ([]datatypes.Virtual_Guest, error)
	GetVirtualGuestsArgs []struct {
		mask    *string
		filters *string
	}
}

func (fake *FakeSoftlayer) GetVirtualGuests(mask, filters *string) ([]datatypes.Virtual_Guest, error) {
	fake.GetVirtualGuestsArgs = append(fake.GetVirtualGuestsArgs, struct {
		mask    *string
		filters *string
	}{mask, filters})
	return fake.GetVirtualGuestsStub(mask, filters)
}

func (fake *FakeSoftlayer) AuthorizeToStorage(storageID, guestID int) error {
	return nil
}

func (fake *FakeSoftlayer) DeauthorizeFromStorage(storageID, guestID int) error {
	return nil
}

func (fake *FakeSoftlayer) GetAllowedStorageVirtualGuests(storageID int) ([]int, error) {
	return []int{}, nil
}

func TestGetIBMCloudInstanceAPIError(t *testing.T) {
	fake := FakeSoftlayer{
		GetVirtualGuestsStub: func(mask, filters *string) (resp []datatypes.Virtual_Guest, err error) {
			return []datatypes.Virtual_Guest{}, fmt.Errorf("Custom error")
		},
	}
	b, err := GetIBMCloudInstances(&fake, nil)
	require.Error(t, err)
	require.Equal(t, "Custom error", err.Error())
	require.Empty(t, b)
}

func TestGetIBMCloudInstancesNoVMs(t *testing.T) {
	fake := FakeSoftlayer{
		GetVirtualGuestsStub: func(mask, filters *string) (resp []datatypes.Virtual_Guest, err error) {
			return []datatypes.Virtual_Guest{}, nil
		},
	}
	tagFilter := "tag-filter"
	b, err := GetIBMCloudInstances(&fake, &tagFilter)
	require.NoError(t, err)
	require.Len(t, b, 0)

	// Verify args
	expectedMask := "id,hostname,primaryIpAddress,primaryBackendIpAddress,tagReferences[id,tag[name]]"
	expectedFilter := filter.New(filter.Path("virtualGuests.tagReferences.tag.name").Eq(&tagFilter)).Build()
	require.Len(t, fake.GetVirtualGuestsArgs, 1)
	require.Equal(t,
		[]struct {
			mask    *string
			filters *string
		}{{&expectedMask, &expectedFilter}},
		fake.GetVirtualGuestsArgs)
}

func TestGetIBMCloudInstances(t *testing.T) {
	vm1 := 1
	publicIP1 := "169.0.0.1"
	backendIP1 := "10.0.0.1"
	hostname1 := "hostname1"

	vm2 := 2
	publicIP2 := "169.0.0.2"
	hostname2 := "hostname2"

	tag1Name := "tag1:val1"
	tag1 := datatypes.Tag{Name: &tag1Name}
	tag2Name := "tag2:val2"
	tag2 := datatypes.Tag{Name: &tag2Name}
	tag3Name := "tag3"
	tag3 := datatypes.Tag{Name: &tag3Name}

	fake := FakeSoftlayer{
		GetVirtualGuestsStub: func(mask, filters *string) (resp []datatypes.Virtual_Guest, err error) {
			return []datatypes.Virtual_Guest{
				{
					Id:                      &vm1,
					PrimaryIpAddress:        &publicIP1,
					PrimaryBackendIpAddress: &backendIP1,
					Hostname:                &hostname1,
				},
				{
					Id:               &vm2,
					PrimaryIpAddress: &publicIP2,
					Hostname:         &hostname2,
					TagReferences:    []datatypes.Tag_Reference{{Tag: &tag1}, {Tag: &tag2}, {Tag: &tag3}},
				}}, nil
		},
	}
	b, err := GetIBMCloudInstances(&fake, nil)
	require.NoError(t, err)
	require.Len(t, b, 2)
	require.Contains(t,
		b,
		backend{
			backendID: "1",
			backendProps: TResourceProperties{
				"ipv4_address":         publicIP1,
				"ipv4_address_private": backendIP1,
				"hostname":             hostname1,
			},
			backendTags: map[string]string{},
			complete:    true,
		})
	require.Contains(t,
		b,
		backend{
			backendID: "2",
			backendProps: TResourceProperties{
				"ipv4_address": publicIP2,
				"hostname":     hostname2,
			},
			backendTags: map[string]string{"tag1": "val1", "tag2": "val2", "tag3": ""},
			complete:    false,
		})

	// Verify args
	expectedMask := "id,hostname,primaryIpAddress,primaryBackendIpAddress,tagReferences[id,tag[name]]"
	require.Len(t, fake.GetVirtualGuestsArgs, 1)
	require.Equal(t,
		[]struct {
			mask    *string
			filters *string
		}{{&expectedMask, nil}},
		fake.GetVirtualGuestsArgs)
}

func TestGetIBMCloudInstancesNoVmID(t *testing.T) {
	vm1 := 1
	publicIP1 := "169.0.0.1"
	backendIP1 := "10.0.0.1"
	hostname1 := "hostname1"

	publicIP2 := "169.0.0.2"
	hostname2 := "hostname2"

	fake := FakeSoftlayer{
		GetVirtualGuestsStub: func(mask, filters *string) (resp []datatypes.Virtual_Guest, err error) {
			return []datatypes.Virtual_Guest{
				{
					Id:                      &vm1,
					PrimaryIpAddress:        &publicIP1,
					PrimaryBackendIpAddress: &backendIP1,
					Hostname:                &hostname1,
				},
				{
					PrimaryIpAddress: &publicIP2,
					Hostname:         &hostname2,
				}}, nil
		},
	}
	b, err := GetIBMCloudInstances(&fake, nil)
	require.Error(t, err)
	require.True(t, strings.HasPrefix(err.Error(), "Returned VM is missing an ID: "))
	require.Empty(t, b)
}
