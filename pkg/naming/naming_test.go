package naming

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestParseConvention(t *testing.T) {
	tests := []struct {
		input    string
		expected Convention
		hasError bool
	}{
		{"direct", Direct, false},
		{"Direct", Direct, false},
		{"DIRECT", Direct, false},
		{"", Auto, false},
		{"snake_case", SnakeCase, false},
		{"Snake_Case", SnakeCase, false},
		{"auto", Auto, false},
		{"AUTO", Auto, false},
		{"default", Auto, false},
		{"DEFAULT", Auto, false},
		{"Default", Auto, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseConvention(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDirectNaming(t *testing.T) {
	conv := Get(Direct)
	assert.Equal(t, "direct", conv.Name())

	tests := []struct {
		input    string
		expected string
	}{
		{"userName", "userName"},
		{"user_name", "user_name"},
		{"CamelCase", "CamelCase"},
		{"123column", "123column"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, conv.Normalize(tt.input))
		})
	}
}

func TestSnakeCaseNaming(t *testing.T) {
	conv := Get(SnakeCase)
	assert.Equal(t, "snake_case", conv.Name())

	tests := []struct {
		input    string
		expected string
	}{
		{"userName", "user_name"},
		{"user_name", "user_name"},
		{"CamelCase", "camel_case"},
		{"camelCase", "camel_case"},
		{"UserName", "user_name"},
		{"HTTPServer", "http_server"},
		{"XMLParser", "xml_parser"},
		{"getHTTPResponse", "get_http_response"},
		{"123column", "_123column"},
		{"column123", "column123"},
		{"column_", "columnx"},
		{"column__", "columnxx"},
		{"column___", "columnxxx"},
		{"column__name", "column__name"},
		{"fields__123count", "fields__123count"},
		{"123fields__count", "_123fields__count"},
		{"__123fields", "_123fields"},
		{"column-name", "column_name"},
		{"column+name", "columnxname"},
		{"column*name", "columnxname"},
		{"column@name", "columnaname"},
		{"column|name", "columnlname"},
		{"column.name", "column_name"},
		{"column name", "column_name"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, conv.Normalize(tt.input))
		})
	}
}

func TestDetectConvention(t *testing.T) {
	t.Run("DetectsSnakeCaseFromCamelCase", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "userId"},
				{Name: "userName"},
				{Name: "createdAt"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "user_name"},
				{Name: "created_at"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("DefaultsToSnakeCaseWhenColumnsAlreadySnakeCase", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "user_name"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "user_name"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("DefaultsToSnakeCaseWhenNoPattern", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "col_a"},
				{Name: "col_b"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "different_col"},
				{Name: "another_col"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("HandlesNilSchemas", func(t *testing.T) {
		assert.Equal(t, SnakeCase, DetectConvention(nil, nil))
		assert.Equal(t, SnakeCase, DetectConvention(&schema.TableSchema{}, nil))
		assert.Equal(t, SnakeCase, DetectConvention(nil, &schema.TableSchema{}))
	})

	t.Run("AllSingleWordColumnsDefaultsToSnakeCase", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "name"},
				{Name: "email"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "name"},
				{Name: "email"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("MostlySingleWordWithOneCamelCaseDetectsSnakeCase", func(t *testing.T) {
		// Single-word columns are ambiguous and skipped, but the one
		// multi-word camelCase column should be enough to detect snake_case.
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "name"},
				{Name: "email"},
				{Name: "createdAt"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "name"},
				{Name: "email"},
				{Name: "created_at"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("MixedColumnsDirectMatch", func(t *testing.T) {
		// Source already has camelCase names and dest keeps them as-is.
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "userName"},
				{Name: "createdAt"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "userName"},
				{Name: "createdAt"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, Direct, result)
	})
}

func TestBuildColumnMapping(t *testing.T) {
	sourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "userId"},
			{Name: "user_name"},
			{Name: "createdAt"},
		},
	}

	t.Run("DirectReturnsEmptyMapping", func(t *testing.T) {
		conv := Get(Direct)
		renames, drops := BuildColumnMapping(sourceSchema, conv)
		assert.Empty(t, renames)
		assert.Empty(t, drops)
	})

	t.Run("SnakeCaseReturnsMapping", func(t *testing.T) {
		conv := Get(SnakeCase)
		renames, drops := BuildColumnMapping(sourceSchema, conv)

		assert.Empty(t, drops)
		assert.Len(t, renames, 2)
		assert.Equal(t, "user_id", renames["userId"])
		assert.Equal(t, "created_at", renames["createdAt"])
		_, exists := renames["user_name"]
		assert.False(t, exists)
	})
}

func TestBuildColumnMappingCollisions(t *testing.T) {
	conv := Get(SnakeCase)

	t.Run("LastWinsEarlierDropped", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "created_at"},
				{Name: "createdAt"},
			},
		}
		renames, drops := BuildColumnMapping(sourceSchema, conv)
		assert.Equal(t, "created_at", renames["createdAt"])
		assert.True(t, drops["created_at"])
		assert.False(t, drops["createdAt"])
		_, ok := renames["id"]
		assert.False(t, ok)
		assert.Equal(t, "created_at", renames["created_at"])
	})

	t.Run("MultipleColumnsAllDroppedExceptLast", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "FirstName"},
				{Name: "FirstNAME"},
				{Name: "First_Name"},
				{Name: "first_name"},
			},
		}
		renames, drops := BuildColumnMapping(sourceSchema, conv)
		// `first_name` (last) is already snake_case → no rename entry, not dropped.
		_, hasLast := renames["first_name"]
		assert.False(t, hasLast)
		assert.False(t, drops["first_name"])
		assert.True(t, drops["FirstName"])
		assert.True(t, drops["FirstNAME"])
		assert.True(t, drops["First_Name"])
		assert.Equal(t, "first_name", renames["FirstName"])
		assert.Equal(t, "first_name", renames["FirstNAME"])
		assert.Equal(t, "first_name", renames["First_Name"])
	})

	t.Run("LastIsCamelStillRenamedTargetSnakeCase", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "created_at"},
				{Name: "createdAt"},
				{Name: "CreatedAt"},
			},
		}
		renames, drops := BuildColumnMapping(sourceSchema, conv)
		assert.Equal(t, "created_at", renames["CreatedAt"])
		assert.True(t, drops["created_at"])
		assert.True(t, drops["createdAt"])
		// Losers also have rename entries pointing at the winner's normalized name.
		assert.Equal(t, "created_at", renames["created_at"])
		assert.Equal(t, "created_at", renames["createdAt"])
	})

	t.Run("LoserHasRenameEntryWhenWinnerIsAlreadySnakeCase", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "userId"},
				{Name: "user_id"},
			},
		}
		renames, drops := BuildColumnMapping(sourceSchema, conv)
		assert.True(t, drops["userId"])
		assert.False(t, drops["user_id"])
		assert.Equal(t, "user_id", renames["userId"],
			"dropped loser must have rename entry to winner's name")
		_, hasWinner := renames["user_id"]
		assert.False(t, hasWinner, "winner (already snake_case) needs no rename")
	})

	t.Run("NoCollision", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "userId"},
				{Name: "createdAt"},
			},
		}
		renames, drops := BuildColumnMapping(sourceSchema, conv)
		assert.Empty(t, drops)
		assert.Equal(t, "user_id", renames["userId"])
		assert.Equal(t, "created_at", renames["createdAt"])
	})
}

func TestIsIngestrColumn(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"_dlt_load_id", true},
		{"_dlt_id", true},
		{"_dlt_parent_id", true},
		{"_dlt_list_idx", true},
		{"_dlt_root_id", true},
		{"_DLT_LOAD_ID", true}, // case insensitive
		{"_Dlt_Id", true},
		{"user_id", false},
		{"dlt_column", false}, // no underscore prefix
		{"_other_column", false},
		{"id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsIngestrColumn(tt.name))
		})
	}
}

func TestHasIngestrColumns(t *testing.T) {
	t.Run("NilSchema", func(t *testing.T) {
		assert.False(t, HasIngestrColumns(nil))
	})

	t.Run("NoIngestrColumns", func(t *testing.T) {
		s := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "user_name"},
			},
		}
		assert.False(t, HasIngestrColumns(s))
	})

	t.Run("WithIngestrColumns", func(t *testing.T) {
		s := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "_dlt_load_id"},
				{Name: "_dlt_id"},
			},
		}
		assert.True(t, HasIngestrColumns(s))
	})
}

func TestGetIngestrColumns(t *testing.T) {
	t.Run("NilSchema", func(t *testing.T) {
		assert.Nil(t, GetIngestrColumns(nil))
	})

	t.Run("ReturnsOnlyIngestrColumns", func(t *testing.T) {
		s := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "_dlt_load_id"},
				{Name: "user_name"},
				{Name: "_dlt_id"},
			},
		}
		cols := GetIngestrColumns(s)
		assert.Len(t, cols, 2)
		assert.Contains(t, cols, "_dlt_load_id")
		assert.Contains(t, cols, "_dlt_id")
	})
}

// Tests for case-insensitive detection (Snowflake returns uppercase column names)
func TestDetectConventionSnowflakeUppercase(t *testing.T) {
	t.Run("SpaceSourceToUppercaseSnakeDest", func(t *testing.T) {
		// Source "APP ID" → dest "APP_ID"
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "date"},
				{Name: "APP ID"},
				{Name: "APP NAME"},
				{Name: "platform"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "DATE"},
				{Name: "APP_ID"},
				{Name: "APP_NAME"},
				{Name: "PLATFORM"},
			},
		}
		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("SpaceSourceToSpaceDestDirect", func(t *testing.T) {
		// Source "APP ID" → dest "APP ID" (ingestr direct preserved spaces)
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "date"},
				{Name: "APP ID"},
				{Name: "APP NAME"},
				{Name: "platform"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "DATE"},
				{Name: "APP ID"},
				{Name: "APP NAME"},
				{Name: "PLATFORM"},
			},
		}
		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, Direct, result)
	})

	t.Run("CamelSourceToUppercaseSnakeDest", func(t *testing.T) {
		// Source "AppName" → dest "APP_NAME"
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "date"},
				{Name: "AppName"},
				{Name: "AppId"},
				{Name: "platform"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "DATE"},
				{Name: "APP_NAME"},
				{Name: "APP_ID"},
				{Name: "PLATFORM"},
			},
		}
		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("UppercaseSourceToUppercaseDestWithIngestrColumns", func(t *testing.T) {
		// ingestr dest with ingestr columns (uppercase from Snowflake)
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "APP ID"},
				{Name: "APP NAME"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "APP_ID"},
				{Name: "APP_NAME"},
				{Name: "_DLT_LOAD_ID"},
				{Name: "_DLT_ID"},
			},
		}
		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})
}

func TestDetectConventionWithIngestrColumns(t *testing.T) {
	t.Run("IgnoresIngestrColumnsInDetection", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "userId"},
				{Name: "userName"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "user_id"},
				{Name: "user_name"},
				{Name: "_dlt_load_id"},
				{Name: "_dlt_id"},
			},
		}

		// Should detect snake_case, ignoring ingestr columns
		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})
}

func TestDetectConventionDoubleUnderscoreSeparator(t *testing.T) {
	t.Run("SnakeCaseDestPreservesDoubleUnderscore", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "fields__name"},
				{Name: "fields__archive"},
				{Name: "fields__creative type"},
				{Name: "fields__drive link (from main creative)"},
				{Name: "fields__last modified by"},
				{Name: "fields__clicks (all)"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "fields__name"},
				{Name: "fields__archive"},
				{Name: "fields__creative_type"},
				{Name: "fields__drive_link_from_main_creativex"},
				{Name: "fields__last_modified_by"},
				{Name: "fields__clicks_allx"},
				{Name: "_dlt_load_id"},
				{Name: "_dlt_id"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})

	t.Run("DirectDestPreservesOriginalNames", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "fields__name"},
				{Name: "fields__archive"},
				{Name: "fields__creative type"},
				{Name: "fields__drive link (from main creative)"},
				{Name: "fields__last modified by"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "fields__name"},
				{Name: "fields__archive"},
				{Name: "fields__creative type"},
				{Name: "fields__drive link (from main creative)"},
				{Name: "fields__last modified by"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, Direct, result)
	})

	t.Run("TrailingUnderscoreXMismatch", func(t *testing.T) {
		sourceSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "spend >>>"},
				{Name: "foo___"},
				{Name: "bar__"},
				{Name: "clicks (all)"},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "spendx"},
				{Name: "fooxxx"},
				{Name: "barxx"},
				{Name: "clicks_allx"},
				{Name: "_dlt_load_id"},
				{Name: "_dlt_id"},
			},
		}

		result := DetectConvention(sourceSchema, destSchema)
		assert.Equal(t, SnakeCase, result)
	})
}
