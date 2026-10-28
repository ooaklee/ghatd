package post

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostMongoStableTimestampPagination(t *testing.T) {
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"created_at_asc", []string{"00", "01", "02", "03", "04"}},
		{"created_at_desc", []string{"04", "03", "02", "01", "00"}},
		{"updated_at_asc", []string{"00", "01", "02", "03", "04"}},
		{"updated_at_desc", []string{"04", "03", "02", "01", "00"}},
		{"published_at_asc", []string{"00", "01", "02", "03", "04"}},
		{"published_at_desc", []string{"04", "03", "02", "01", "00"}},
		{"deleted_at_asc", []string{"00", "01", "02", "03", "04"}},
		{"deleted_at_desc", []string{"04", "03", "02", "01", "00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx := isolatedPostMongo(t)
			for i := 4; i >= 0; i-- {
				_, err := db.Collection(PostCollection).InsertOne(ctx, Post{
					Id: fmt.Sprintf("%02d", i), Type: PostTypeChangelog,
					CreatedAt: "2026-10-28T12:00:00.000000000", UpdatedAt: "2026-10-28T12:00:00.000000000",
					PublishedAt: "2026-10-28T12:00:00.000000000", DeletedAt: "2026-10-28T12:00:00.000000000",
				})
				require.NoError(t, err)
			}
			for range 2 {
				var ids []string
				for page := 1; page <= 3; page++ {
					items, err := repo.GetPosts(ctx, &GetPostsRequest{Order: tc.name, PerPage: 2, Page: page})
					require.NoError(t, err)
					for _, item := range items {
						ids = append(ids, item.Id)
					}
				}
				require.Equal(t, tc.want, ids, "tied timestamps must neither repeat nor omit records across pages")
			}
		})
	}
}
