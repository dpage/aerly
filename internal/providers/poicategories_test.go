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

// TestClassifyMatchesDescendantCodes pins the codeMatches prefix branch: a
// Geoapify feature code that is a descendant of a mapped code (rather than
// an exact match to it) must still classify correctly. A prior test covering
// this branch was deleted and never translated, leaving it unproven.
func TestClassifyMatchesDescendantCodes(t *testing.T) {
	cases := []struct {
		codes []string
		want  string
	}{
		// "catering.restaurant.pizza" is a descendant of the mapped
		// "catering.restaurant".
		{[]string{"catering.restaurant.pizza"}, "restaurants"},
		// "heritage.unesco" is a descendant of the mapped "heritage".
		{[]string{"heritage.unesco"}, "monuments_heritage"},
		// "natural.forest" is a descendant of the mapped "natural".
		{[]string{"natural.forest"}, "natural_features"},
		// "tourism.attraction.viewpoint.tower" is a descendant of the mapped
		// "tourism.attraction.viewpoint", and also a descendant of the
		// "attractions" code "tourism.attraction". This pins both the prefix
		// match itself and that viewpoints still takes precedence over the
		// more generic attractions in classifyOrder.
		{[]string{"tourism.attraction.viewpoint.tower"}, "viewpoints"},
	}
	for _, c := range cases {
		if got := Classify(c.codes); got != c.want {
			t.Errorf("Classify(%v) = %q, want %q", c.codes, got, c.want)
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
