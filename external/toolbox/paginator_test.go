package toolbox_test

import (
	"context"
	"errors"
	"log"
	"testing"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/stretchr/testify/assert"
)

func TestToolbox_GetResourcePagination(t *testing.T) {

	testSlice := []int{
		1,
		2,
		3,
		4,
		5,
		6,
		7,
		8,
		9,
		10,
	}

	testSliceOne := []int{
		1,
	}

	testSliceSeven := []int{
		1,
		2,
		3,
		4,
		5,
		6,
		7,
	}

	tests := []struct {
		name                   string
		sourceSlice            []int
		paginationRequest      toolbox.GetResourcePaginationRequest
		totalNumberOfResources int
		expectedError          error
		expectedCollection     []int
		expectedTotal          int
		expectedTotalPages     int

		expectedResourcePerPage int
		expectedPage            int
	}{
		{
			name:        "Success - Reported bug",
			sourceSlice: []int{6, 7},
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 5,
				Page:    2,
			},
			totalNumberOfResources: len(testSliceSeven),
			expectedError:          nil,
			expectedCollection:     []int{6, 7},
			expectedTotal:          7,
			expectedTotalPages:     2,
		},
		{
			name:        "Success - One",
			sourceSlice: []int{1},
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 1,
				Page:    1,
			},
			totalNumberOfResources: len(testSlice),
			expectedError:          nil,
			expectedCollection:     []int{1},
			expectedTotal:          10,
			expectedTotalPages:     10,
		},
		{
			name:        "Success - One",
			sourceSlice: []int{1},
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 10,
				Page:    1,
			},
			totalNumberOfResources: len(testSliceOne),
			expectedError:          nil,
			expectedCollection:     []int{1},
			expectedTotal:          1,
			expectedTotalPages:     1,
		},
		{
			name:        "Success - 7",
			sourceSlice: []int{8, 9, 10},
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 7,
				Page:    2,
			},
			totalNumberOfResources: len(testSlice),
			expectedError:          nil,
			expectedCollection:     []int{8, 9, 10},
			expectedTotal:          10,
			expectedTotalPages:     2,
		},
		{
			name:        "Success - 10",
			sourceSlice: testSlice,
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 10,
				Page:    1,
			},
			totalNumberOfResources: len(testSlice),
			expectedError:          nil,
			expectedCollection:     testSlice,
			expectedTotal:          10,
			expectedTotalPages:     1,
		},
		{
			name:        "Success - 3",
			sourceSlice: []int{4, 5, 6},
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 3,
				Page:    2,
			},
			totalNumberOfResources: len(testSlice),
			expectedError:          nil,
			expectedCollection:     []int{4, 5, 6},
			expectedTotal:          10,
			expectedTotalPages:     4,
		},
		{
			name:        "Failure - Out of range",
			sourceSlice: testSlice,
			paginationRequest: toolbox.GetResourcePaginationRequest{
				PerPage: 7,
				Page:    3,
			},
			expectedError: errors.New(toolbox.ErrKeyPageOutOfRange),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			var sourceToInterfaceSlice []interface{}

			for _, element := range test.sourceSlice {
				sourceToInterfaceSlice = append(sourceToInterfaceSlice, element)
			}

			result, err := toolbox.GetResourcePagination(context.Background(), &test.paginationRequest, sourceToInterfaceSlice, test.totalNumberOfResources)

			assert.Equal(t, test.expectedError, err)

			if test.expectedError == nil {

				var castedResources []int

				for _, resource := range result.Resources {
					castedResource, ok := resource.(int)
					if !ok {
						log.Fatal("failed-to-cast-test-result")
					}
					castedResources = append(castedResources, castedResource)
				}

				assert.Equal(t, test.expectedCollection, castedResources)
				assert.Equal(t, test.expectedTotal, result.Total)
				assert.Equal(t, test.expectedTotalPages, result.TotalPages)
				assert.Equal(t, test.paginationRequest.PerPage, result.ResourcePerPage)
			}

		})
	}

}
