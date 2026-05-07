package destination

import (
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortenIdentifier(t *testing.T) {
	t.Run("under limit unchanged", func(t *testing.T) {
		assert.Equal(t, "short_name", ShortenIdentifier("short_name", "short_name", 63))
	})

	t.Run("at exact limit unchanged", func(t *testing.T) {
		name := "a_name_that_is_exactly_sixty_three_bytes_long_padded_to_63_char"
		assert.Len(t, name, 63)
		assert.Equal(t, name, ShortenIdentifier(name, name, 63))
	})

	t.Run("over limit shortened", func(t *testing.T) {
		name := "results_number_of_individuals_or_communities_trained_on_internet_use"
		result := ShortenIdentifier(name, name, 63)
		assert.LessOrEqual(t, len(result), 63)
		assert.NotEqual(t, name, result)
		assert.Contains(t, result, "_")
	})

	t.Run("shortened name preserves prefix and suffix", func(t *testing.T) {
		name := "results_number_of_individuals_or_communities_trained_on_internet_use"
		result := ShortenIdentifier(name, name, 63)
		assert.Equal(t, 63, len(result))
		// Tag is 6 chars, remaining=57, overflow=1, prefix=29, suffix=28
		assert.Equal(t, name[:29], result[:29], "prefix should be preserved")
		assert.Equal(t, name[len(name)-28:], result[len(result)-28:], "suffix should be preserved")
	})

	t.Run("two colliding names produce different results", func(t *testing.T) {
		name1 := "results_number_of_individuals_or_communities_trained_on_internet_use"
		name2 := "results_number_of_individuals_or_communities_trained_on_internet_use_1"
		r1 := ShortenIdentifier(name1, name1, 63)
		r2 := ShortenIdentifier(name2, name2, 63)
		assert.NotEqual(t, r1, r2, "different original names should produce different shortened names")
		assert.LessOrEqual(t, len(r1), 63)
		assert.LessOrEqual(t, len(r2), 63)
	})

	t.Run("maxLen zero means no shortening", func(t *testing.T) {
		name := "a_very_long_column_name_that_exceeds_all_reasonable_limits_for_database_identifiers"
		assert.Equal(t, name, ShortenIdentifier(name, name, 0))
	})

	t.Run("negative maxLen means no shortening", func(t *testing.T) {
		name := "long_name"
		assert.Equal(t, name, ShortenIdentifier(name, name, -1))
	})

	t.Run("very short maxLen", func(t *testing.T) {
		name := "a_very_long_column_name"
		result := ShortenIdentifier(name, name, 10)
		assert.LessOrEqual(t, len(result), 10)
	})

	t.Run("deterministic", func(t *testing.T) {
		name := "results_number_of_individuals_or_communities_trained_on_internet_use"
		r1 := ShortenIdentifier(name, name, 63)
		r2 := ShortenIdentifier(name, name, 63)
		assert.Equal(t, r1, r2, "same input should always produce same output")
	})

	t.Run("hash uses hashSource not name", func(t *testing.T) {
		name := "results_number_of_individuals_or_communities_trained_on_internet_use"
		r1 := ShortenIdentifier(name, name, 63)
		r2 := ShortenIdentifier(name, "originalCamelCaseName", 63)
		assert.NotEqual(t, r1, r2, "different hashSource should produce different tags")
		assert.Equal(t, 63, len(r1))
		assert.Equal(t, 63, len(r2))
	})
}

func TestShortenColumnNames(t *testing.T) {
	t.Run("no shortening needed", func(t *testing.T) {
		columns := []schema.Column{
			{Name: "id"},
			{Name: "name"},
			{Name: "active"},
		}
		mapping := ShortenColumnNames(columns, 63, nil)
		assert.Nil(t, mapping)
	})

	t.Run("maxLen zero returns nil", func(t *testing.T) {
		columns := []schema.Column{
			{Name: "a_very_long_column_name_that_exceeds_sixty_three_bytes_for_sure_right"},
		}
		mapping := ShortenColumnNames(columns, 0, nil)
		assert.Nil(t, mapping)
	})

	t.Run("shortens long columns only", func(t *testing.T) {
		columns := []schema.Column{
			{Name: "id"},
			{Name: "results_number_of_individuals_or_communities_trained_on_internet_use"},
			{Name: "short_col"},
		}
		mapping := ShortenColumnNames(columns, 63, nil)
		require.NotNil(t, mapping)
		assert.Len(t, mapping, 1)
		assert.Contains(t, mapping, "results_number_of_individuals_or_communities_trained_on_internet_use")
		shortened := mapping["results_number_of_individuals_or_communities_trained_on_internet_use"]
		assert.LessOrEqual(t, len(shortened), 63)
	})

	t.Run("two similar long columns get different shortened names", func(t *testing.T) {
		columns := []schema.Column{
			{Name: "results_number_of_individuals_or_communities_trained_on_internet_use"},
			{Name: "results_number_of_individuals_or_communities_trained_on_internet_use_1"},
		}
		mapping := ShortenColumnNames(columns, 63, nil)
		require.NotNil(t, mapping)
		assert.Len(t, mapping, 2)

		short1 := mapping["results_number_of_individuals_or_communities_trained_on_internet_use"]
		short2 := mapping["results_number_of_individuals_or_communities_trained_on_internet_use_1"]
		assert.NotEqual(t, short1, short2, "different hashes ensure unique shortened names")
		assert.LessOrEqual(t, len(short1), 63)
		assert.LessOrEqual(t, len(short2), 63)
	})

	t.Run("shortening is deterministic across runs", func(t *testing.T) {
		columns := []schema.Column{
			{Name: "results_number_of_individuals_or_communities_trained_on_internet_use"},
			{Name: "results_number_of_individuals_or_communities_trained_on_internet_use_1"},
		}
		first := ShortenColumnNames(columns, 63, nil)
		require.NotNil(t, first)
		for i := 0; i < 100; i++ {
			mapping := ShortenColumnNames(columns, 63, nil)
			assert.Equal(t, first, mapping, "output must be deterministic across runs")
		}
	})

	t.Run("uses renameMapping for hash when provided", func(t *testing.T) {
		columns := []schema.Column{
			{Name: "results_number_of_individuals_or_communities_trained_on_internet_use"},
		}
		// Forward mapping: original → normalized (same as columnRenamer.Mapping())
		renameMapping := map[string]string{
			"resultsNumberOfIndividualsOrCommunitiesTrainedOnInternetUse": "results_number_of_individuals_or_communities_trained_on_internet_use",
		}
		withOrig := ShortenColumnNames(columns, 63, renameMapping)
		withoutOrig := ShortenColumnNames(columns, 63, nil)
		require.NotNil(t, withOrig)
		require.NotNil(t, withoutOrig)
		assert.NotEqual(t, withOrig[columns[0].Name], withoutOrig[columns[0].Name],
			"different hash source should produce different shortened names")
	})
}
