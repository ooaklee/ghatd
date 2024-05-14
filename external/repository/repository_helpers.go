package repository

// GetPaginationLimit gets the pagination limit from passed params and returns
// a pointer
func GetPaginationLimit(numberOfResourcePerPage int64) *int64 {
	var paginationLimit int64 = 0

	paginationLimit = numberOfResourcePerPage

	return &paginationLimit
}

// GetPaginationSkip calculates the skip value for pagination based on the
// page number and limit passed
func GetPaginationSkip(pageNumber int64, paginationLimit *int64) *int64 {
	var skip int64 = 0

	if pageNumber > 1 {
		skip = (pageNumber - 1) * *paginationLimit
	}

	return &skip
}
