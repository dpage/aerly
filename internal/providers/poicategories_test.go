package providers

import "testing"

func TestSubcategoryCodesUnionsAndDedupes(t *testing.T) {
	got := SubcategoryCodes([]string{"restaurants", "bars", "restaurants"})
	want := map[string]bool{"catering.restaurant": true, "catering.bar": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, c := range got {
		if !want[c] {
			t.Fatalf("unexpected code %q in %v", c, got)
		}
	}
}

func TestEveryThemeChildIsAKnownSubcategory(t *testing.T) {
	valid := ValidSubcategoryKeys()
	for theme, kids := range themeSubcategories {
		if len(kids) == 0 {
			t.Errorf("theme %q has no sub-categories", theme)
		}
		for _, k := range kids {
			if !valid[k] {
				t.Errorf("theme %q references unknown sub-category %q", theme, k)
			}
			if len(subcategoryCodes[k]) == 0 {
				t.Errorf("sub-category %q maps to no Geoapify codes", k)
			}
		}
	}
}

func TestThemeOrderMatchesThemeSubcategories(t *testing.T) {
	if len(themeOrder) != len(themeSubcategories) {
		t.Fatalf("themeOrder has %d entries, themeSubcategories has %d", len(themeOrder), len(themeSubcategories))
	}
	for _, th := range themeOrder {
		if _, ok := themeSubcategories[th]; !ok {
			t.Errorf("themeOrder lists unknown theme %q", th)
		}
	}
}

func TestClassifyPrefersMostSpecificAndNeverEmpty(t *testing.T) {
	cases := []struct {
		codes []string
		want  string
	}{
		{[]string{"catering.restaurant"}, "restaurants"},
		{[]string{"catering.bar"}, "bars"},
		{[]string{"adult.nightclub"}, "nightclubs"},
		{[]string{"religion.place_of_worship", "heritage"}, "worship"}, // worship wins over heritage
		{[]string{"entertainment.museum"}, "museums"},
		{[]string{"leisure.park"}, "parks_gardens"},
		{[]string{"something.unmapped"}, "attractions"}, // sensible default, never ""
	}
	for _, c := range cases {
		if got := Classify(c.codes); got != c.want {
			t.Errorf("Classify(%v) = %q, want %q", c.codes, got, c.want)
		}
	}
}
