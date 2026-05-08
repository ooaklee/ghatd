package errormanifest

import (
	"errors"
	"testing"

	"github.com/ooaklee/reply/v2"
)

var (
	errA = errors.New("a")
	errB = errors.New("b")
	errC = errors.New("a")
	errD = errors.New("d")

	mapA = reply.ErrorManifest{errA: {Title: "A1", StatusCode: 400}}
	mapB = reply.ErrorManifest{errB: {Title: "B1", StatusCode: 401}}
	mapC = reply.ErrorManifest{errC: {Title: "A2", StatusCode: 402}}
	mapD = reply.ErrorManifest{errD: {Title: "D1", StatusCode: 403}}
)

func TestComposerBuild(t *testing.T) {
	c := NewComposer()
	c.Add(mapA, mapB)

	result := c.Build()
	if len(result) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(result))
	}
	if result[0][errA].Title != "A1" {
		t.Errorf("expected base manifest at index 0")
	}
	if result[1][errB].Title != "B1" {
		t.Errorf("expected base manifest at index 1")
	}
}

func TestComposerOverrideLastWins(t *testing.T) {
	c := NewComposer()
	c.Add(mapA, mapB)
	c.AddOverrides(mapC)

	result := c.Build()
	if len(result) != 3 {
		t.Fatalf("expected 3 manifests, got %d", len(result))
	}
	if result[0][errA].Title != "A1" {
		t.Errorf("expected original at index 0")
	}
	if result[2][errC].Title != "A2" {
		t.Errorf("expected override at index 2 (last), got index with Title=%s", result[2][errA].Title)
	}
}

func TestComposerDuplicates(t *testing.T) {
	c := NewComposer()
	c.Add(mapA, mapC)

	dups := c.Duplicates()
	if len(dups) != 1 || dups[0] != "a" {
		t.Fatalf("expected ['a'], got %v", dups)
	}
}

func TestComposerNoDuplicatesWhenDistinct(t *testing.T) {
	c := NewComposer()
	c.Add(mapA, mapB)

	dups := c.Duplicates()
	if len(dups) != 0 {
		t.Fatalf("expected no duplicates, got %v", dups)
	}
}

func TestComposerDuplicatesAcrossBaseAndOverrides(t *testing.T) {
	c := NewComposer()
	c.Add(mapA)
	c.AddOverrides(mapC)

	dups := c.Duplicates()
	if !contains(dups, "a") {
		t.Fatalf("expected 'a' in duplicates, got %v", dups)
	}
}

func TestComposerOverridesOnlyWithExclusiveKeys(t *testing.T) {
	c := NewComposer()
	c.Add(mapA)
	c.AddOverrides(mapD)

	result := c.Build()
	if len(result) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(result))
	}
	if result[0][errA].Title != "A1" {
		t.Errorf("expected base at index 0")
	}
	if result[1][errD].Title != "D1" {
		t.Errorf("expected override at index 1")
	}
}

func TestComposerReset(t *testing.T) {
	c := NewComposer()
	c.Add(mapA)

	if len(c.Build()) != 1 {
		t.Fatal("expected 1 after add")
	}

	c.Reset()

	if len(c.Build()) != 0 {
		t.Fatal("expected 0 after reset")
	}
}

func TestComposerOutputIsCompatibleWithNewReplier(t *testing.T) {
	c := NewComposer()
	c.Add(mapA, mapB)
	c.AddOverrides(mapC)

	replier := reply.NewReplier(c.Build())
	if replier == nil {
		t.Fatal("expected non-nil Replier")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
