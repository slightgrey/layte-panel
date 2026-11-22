package store

import (
	"context"
	"testing"
)

func TestDatabasesForSite(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a, err := s.CreateSite(ctx, Site{Domain: "a.test", SystemUser: "atest", Managed: true, Source: "created", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateSite(ctx, Site{Domain: "b.test", SystemUser: "btest", Managed: true, Source: "created", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []Database{
		{Name: "atest", User: "atest", SiteID: a},
		{Name: "a_extra", User: "a_extra", SiteID: a},
		{Name: "btest", User: "btest", SiteID: b},
		{Name: "loose", User: "loose"},
	} {
		if err := s.AddDatabase(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.DatabasesForSite(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	if len(names) != 2 || names[0] != "a_extra" || names[1] != "atest" {
		t.Errorf("DatabasesForSite(a) = %v, want [a_extra atest]", names)
	}

	// Deleting the site keeps its databases but unlinks them (ON DELETE SET NULL).
	if err := s.DeleteSite(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DatabasesForSite(ctx, a); len(got) != 0 {
		t.Errorf("after delete: %d databases still linked", len(got))
	}
	if d, err := s.DatabaseByName(ctx, "a_extra"); err != nil || d.SiteID != 0 {
		t.Errorf("a_extra after delete = %+v, %v; want kept and unlinked", d, err)
	}
}
