package maxids

import "testing"

func TestRandomNamer_Shape(t *testing.T) {
	n, err := NewDefaultNamer(DefaultStyleRandom)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		name, err := n.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !n.Owns(name) {
			t.Fatalf("%q not owned by random namer", name)
		}
		got, err := Normalize(name)
		if err != nil || got != name {
			t.Fatalf("Normalize(%q) = %q, %v", name, got, err)
		}
		if seen[name] {
			t.Fatalf("duplicate name %q", name)
		}
		seen[name] = true
	}
}

func TestIsDefaultShape(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"u7k3q9x2m4tab8zz", true},
		{"quiet-river-4821", true},
		{"alice", false},
		{"quiet-river-48", false},
		{"abcdefghjkmnpq23", false},  // only 2 digits
		{"abcdefghjkmnpq2a", false},  // only 1 digit
		{"abcdefghjkmnpq23x", false}, // 17 chars
		{"abcdefghjkmnp234", true},
		{"abcdefghjkmnpq22", false}, // only 2 digits
		{"u7k3q9x2m4tab8zo", false}, // contains o
		{"u7k3q9x2m4tab8z1", false}, // contains 1
		{"2345678923456789", false}, // no letters
	}
	for _, c := range cases {
		if got := IsDefaultShape(c.in); got != c.want {
			t.Errorf("IsDefaultShape(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNewDefaultNamer(t *testing.T) {
	if _, err := NewDefaultNamer("random"); err != nil {
		t.Fatalf("random: %v", err)
	}
	for _, s := range []string{"words", "bogus"} {
		if _, err := NewDefaultNamer(s); err == nil {
			t.Fatalf("%s: expected error", s)
		}
	}
}
