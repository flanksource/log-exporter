package opensearch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IsJSONQuery detects if the provided query string is a JSON query
func IsJSONQuery(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" || query == "*" {
		return false
	}

	// Check if it starts with '{' indicating JSON
	if !strings.HasPrefix(query, "{") {
		return false
	}

	// Validate it's proper JSON
	var jsonQuery map[string]interface{}
	return json.Unmarshal([]byte(query), &jsonQuery) == nil
}

// ValidateJSONQuery validates that the JSON query has a valid structure
func ValidateJSONQuery(query string) error {
	var jsonQuery map[string]interface{}
	if err := json.Unmarshal([]byte(query), &jsonQuery); err != nil {
		return fmt.Errorf("invalid JSON query: %w", err)
	}

	// Check if it's a complete OpenSearch query or just a query clause
	if _, hasQuery := jsonQuery["query"]; hasQuery {
		// It's a complete query with top-level structure
		return nil
	}

	// Check if it's a query clause (match, bool, term, etc.)
	validQueryTypes := []string{
		"match", "match_all", "match_phrase", "match_phrase_prefix",
		"multi_match", "query_string", "simple_query_string",
		"term", "terms", "range", "exists", "prefix", "wildcard",
		"regexp", "fuzzy", "bool", "constant_score", "dis_max",
		"function_score", "boosting", "nested", "has_child",
		"has_parent", "percolate", "more_like_this",
	}

	for queryType := range jsonQuery {
		for _, validType := range validQueryTypes {
			if queryType == validType {
				return nil
			}
		}
	}

	// Check for aggregations
	if _, hasAggs := jsonQuery["aggs"]; hasAggs {
		return nil
	}
	if _, hasAggregations := jsonQuery["aggregations"]; hasAggregations {
		return nil
	}

	return fmt.Errorf("JSON query does not contain recognized query types or aggregations")
}

// MergeJSONWithTimeRange merges time range constraints into an existing JSON query
func MergeJSONWithTimeRange(jsonQuery string, timestampField, fromTime, toTime string) (string, error) {
	// If no time range specified, return query as-is
	if fromTime == "" && toTime == "" {
		return jsonQuery, nil
	}

	var query map[string]interface{}
	if err := json.Unmarshal([]byte(jsonQuery), &query); err != nil {
		return "", fmt.Errorf("failed to parse JSON query: %w", err)
	}

	// Create time range query
	timeRangeQuery := map[string]interface{}{
		"range": map[string]interface{}{
			timestampField: map[string]interface{}{},
		},
	}

	if fromTime != "" {
		timeRangeQuery["range"].(map[string]interface{})[timestampField].(map[string]interface{})["gte"] = fromTime
	}
	if toTime != "" {
		timeRangeQuery["range"].(map[string]interface{})[timestampField].(map[string]interface{})["lte"] = toTime
	}

	// Check if this is a complete query or just a query clause
	if _, hasQuery := query["query"]; hasQuery {
		// It's a complete query, merge into existing structure
		return mergeTimeRangeIntoCompleteQuery(query, timeRangeQuery)
	} else {
		// It's just a query clause, wrap it in a complete query structure
		return wrapQueryClauseWithTimeRange(query, timeRangeQuery)
	}
}

// mergeTimeRangeIntoCompleteQuery merges time range into a complete query structure
func mergeTimeRangeIntoCompleteQuery(query map[string]interface{}, timeRangeQuery map[string]interface{}) (string, error) {
	queryClause := query["query"].(map[string]interface{})

	// If the existing query is already a bool query, add time range to must clause
	if boolQuery, isBool := queryClause["bool"]; isBool {
		boolMap := boolQuery.(map[string]interface{})
		if mustClause, hasMust := boolMap["must"]; hasMust {
			// Add to existing must array
			mustArray := mustClause.([]interface{})
			boolMap["must"] = append(mustArray, timeRangeQuery)
		} else {
			// Create must array with existing query and time range
			boolMap["must"] = []interface{}{timeRangeQuery}
		}
	} else {
		// Wrap existing query in bool with time range
		query["query"] = map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{queryClause, timeRangeQuery},
			},
		}
	}

	resultBytes, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged query: %w", err)
	}

	return string(resultBytes), nil
}

// wrapQueryClauseWithTimeRange wraps a query clause in a complete query structure with time range
func wrapQueryClauseWithTimeRange(queryClause map[string]interface{}, timeRangeQuery map[string]interface{}) (string, error) {
	completeQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{queryClause, timeRangeQuery},
			},
		},
	}

	resultBytes, err := json.Marshal(completeQuery)
	if err != nil {
		return "", fmt.Errorf("failed to marshal wrapped query: %w", err)
	}

	return string(resultBytes), nil
}

// AddFieldFilteringToJSON adds field filtering (_source) to a JSON query
func AddFieldFilteringToJSON(jsonQuery string, fields []string) (string, error) {
	if len(fields) == 0 {
		return jsonQuery, nil
	}

	var query map[string]interface{}
	if err := json.Unmarshal([]byte(jsonQuery), &query); err != nil {
		return "", fmt.Errorf("failed to parse JSON query: %w", err)
	}

	query["_source"] = fields

	resultBytes, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query with field filtering: %w", err)
	}

	return string(resultBytes), nil
}

// AddSortingToJSON adds sorting to a JSON query
func AddSortingToJSON(jsonQuery string, timestampField string) (string, error) {
	var query map[string]interface{}
	if err := json.Unmarshal([]byte(jsonQuery), &query); err != nil {
		return "", fmt.Errorf("failed to parse JSON query: %w", err)
	}

	// Only add sorting if not already present
	if _, hasSort := query["sort"]; !hasSort {
		query["sort"] = []interface{}{
			map[string]interface{}{
				timestampField: map[string]interface{}{
					"order": "desc",
				},
			},
		}
	}

	resultBytes, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query with sorting: %w", err)
	}

	return string(resultBytes), nil
}

// AddFiltersToQuery adds filter constraints to a query (both JSON and Lucene)
func AddFiltersToQuery(query string, constraints []FilterConstraint) (string, error) {
	if len(constraints) == 0 {
		return query, nil
	}

	if IsJSONQuery(query) {
		return addFiltersToJSONQuery(query, constraints)
	} else {
		return addFiltersToLuceneQuery(query, constraints)
	}
}

// addFiltersToJSONQuery adds filter constraints to a JSON query
func addFiltersToJSONQuery(jsonQuery string, constraints []FilterConstraint) (string, error) {
	var query map[string]interface{}
	if err := json.Unmarshal([]byte(jsonQuery), &query); err != nil {
		return "", fmt.Errorf("failed to parse JSON query: %w", err)
	}

	// Create filter terms for each constraint
	var filterTerms []interface{}
	for _, constraint := range constraints {
		filterTerms = append(filterTerms, map[string]interface{}{
			"term": map[string]interface{}{
				constraint.Field: constraint.Value,
			},
		})
	}

	// Check if this is a complete query or just a query clause
	if _, hasQuery := query["query"]; hasQuery {
		// It's a complete query, merge into existing structure
		return mergeFiltersIntoCompleteQuery(query, filterTerms)
	} else {
		// It's just a query clause, wrap it in a complete query structure
		return wrapQueryClauseWithFilters(query, filterTerms)
	}
}

// mergeFiltersIntoCompleteQuery merges filter terms into a complete query structure
func mergeFiltersIntoCompleteQuery(query map[string]interface{}, filterTerms []interface{}) (string, error) {
	queryClause := query["query"].(map[string]interface{})

	// If the existing query is already a bool query, add filters to must clause
	if boolQuery, isBool := queryClause["bool"]; isBool {
		boolMap := boolQuery.(map[string]interface{})
		if mustClause, hasMust := boolMap["must"]; hasMust {
			// Add to existing must array
			mustArray := mustClause.([]interface{})
			boolMap["must"] = append(mustArray, filterTerms...)
		} else {
			// Create must array with filters
			boolMap["must"] = filterTerms
		}
	} else {
		// Wrap existing query in bool with filters
		allMustTerms := append([]interface{}{queryClause}, filterTerms...)
		query["query"] = map[string]interface{}{
			"bool": map[string]interface{}{
				"must": allMustTerms,
			},
		}
	}

	resultBytes, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query with filters: %w", err)
	}

	return string(resultBytes), nil
}

// wrapQueryClauseWithFilters wraps a query clause in a complete query structure with filters
func wrapQueryClauseWithFilters(queryClause map[string]interface{}, filterTerms []interface{}) (string, error) {
	allMustTerms := append([]interface{}{queryClause}, filterTerms...)
	completeQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": allMustTerms,
			},
		},
	}

	resultBytes, err := json.Marshal(completeQuery)
	if err != nil {
		return "", fmt.Errorf("failed to marshal wrapped query with filters: %w", err)
	}

	return string(resultBytes), nil
}

// addFiltersToLuceneQuery adds filter constraints to a Lucene query string
func addFiltersToLuceneQuery(luceneQuery string, constraints []FilterConstraint) (string, error) {
	if luceneQuery == "*" || strings.TrimSpace(luceneQuery) == "" {
		// Replace wildcard with filter terms
		var terms []string
		for _, constraint := range constraints {
			// Escape field value for Lucene syntax
			escapedValue := escapeQueryValue(constraint.Value)
			terms = append(terms, fmt.Sprintf("%s:%s", constraint.Field, escapedValue))
		}
		return strings.Join(terms, " AND "), nil
	}

	// Add filter terms to existing query
	var terms []string
	terms = append(terms, "("+luceneQuery+")")

	for _, constraint := range constraints {
		escapedValue := escapeQueryValue(constraint.Value)
		terms = append(terms, fmt.Sprintf("%s:%s", constraint.Field, escapedValue))
	}

	return strings.Join(terms, " AND "), nil
}

// escapeQueryValue escapes special characters in query values
func escapeQueryValue(value string) string {
	// If value contains spaces or special characters, wrap in quotes
	if strings.ContainsAny(value, " \t\n+-=&&||><!(){}[]^\"~*?:\\/") {
		// Escape quotes within the value
		escaped := strings.ReplaceAll(value, "\"", "\\\"")
		return fmt.Sprintf("\"%s\"", escaped)
	}
	return value
}
