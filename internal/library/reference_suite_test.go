package library

import (
	"context"
	"os"
	"testing"
)

func TestReferencedSuiteNeverCopiesOrRemovesOriginal(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	external := entry(t, f, "external", "external original")
	external.Reference = true
	first := Manifest{Version: 1, ID: "reference-suite", GenreID: "fighting", Games: []Entry{external}}
	plan, err := f.m.Plan(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AcquireBytes != 0 {
		t.Fatal("reference should not be copied")
	}
	op, err := f.m.Submit(ctx, Request{Key: "reference-switch", Kind: "switch", Switch: &SwitchRequest{Manifest: first, ExpectedRevision: plan.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	op = waitOperation(t, f.m, op)
	if op.State != "completed" {
		t.Fatal(op)
	}
	games, err := f.m.Games(ctx)
	if err != nil || len(games) != 1 || games[0].Ownership != "external" || games[0].ArtifactID != "" || games[0].Path != external.Source.Path {
		t.Fatal(games, err)
	}
	second := Manifest{Version: 1, ID: "managed-suite", GenreID: "platform", Games: []Entry{entry(t, f, "managed", "managed content")}}
	plan, err = f.m.Plan(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	op, err = f.m.Submit(ctx, Request{Key: "managed-switch", Kind: "switch", Switch: &SwitchRequest{Manifest: second, ExpectedRevision: plan.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	op = waitOperation(t, f.m, op)
	if op.State != "completed" {
		t.Fatal(op)
	}
	b, err := os.ReadFile(external.Source.Path)
	if err != nil || string(b) != "external original" {
		t.Fatal("original removed or modified", err)
	}
}

func TestChangedReferenceFailsBeforeRemovingCurrentGames(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	managed := entry(t, f, "current", "keep this game")
	if op := switchTo(t, f, "platform", managed); op.State != "completed" {
		t.Fatal(op)
	}
	before, err := f.m.Games(ctx)
	if err != nil || len(before) != 1 {
		t.Fatal(before, err)
	}
	external := entry(t, f, "external", "original")
	external.Reference = true
	manifest := Manifest{Version: 1, ID: "bad-reference", GenreID: "fighting", Games: []Entry{external}}
	plan, err := f.m.Plan(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Equal length defeats a size-only preflight. The worker must check the
	// reference's digest before it removes any currently installed game.
	if err = os.WriteFile(external.Source.Path, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	op := submit(t, f, Request{Kind: "switch", Switch: &SwitchRequest{Manifest: manifest, ExpectedRevision: plan.Revision}})
	if op.State != "failed" {
		t.Fatal("changed reference accepted", op)
	}
	content, err := os.ReadFile(before[0].Path)
	if err != nil || string(content) != "keep this game" {
		t.Fatal("current game removed before reference validation", err)
	}
}
