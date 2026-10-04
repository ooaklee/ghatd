// Package voter provides shared, actor-specific voting for authorized domain
// services. Service depends on a driver-free repository port; Repository uses
// managed Mongo helpers and the votes collection. It exposes no HTTP surface.
// Consumers own resource authorization, existence and downvote policy. See the
// package README for composition and the explicit index migration.
package voter
