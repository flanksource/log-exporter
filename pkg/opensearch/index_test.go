package opensearch

import (
	"testing"
	"time"
)

func TestExtractDateFromIndex(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name        string
		indexName   string
		datePattern string
		want        *time.Time
	}{
		{
			name:        "logstash style date",
			indexName:   "logstash-2024.01.15",
			datePattern: "2006.01.02",
			want:        timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:        "filebeat style date",
			indexName:   "filebeat-2024-01-15",
			datePattern: "2006-01-02",
			want:        timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:        "monthly index",
			indexName:   "logs-2024.01",
			datePattern: "2006.01",
			want:        timePtr(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:        "compact date format",
			indexName:   "logs20240115",
			datePattern: "20060102",
			want:        timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:        "no date in index",
			indexName:   "system-logs",
			datePattern: "2006.01.02",
			want:        nil,
		},
		{
			name:        "jaeger with date",
			indexName:   "jaeger-span-2024.01.15",
			datePattern: "2006.01.02",
			want:        timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.extractDateFromIndex(tt.indexName, tt.datePattern)

			if tt.want == nil && got != nil {
				t.Errorf("extractDateFromIndex() = %v, want nil", got)
				return
			}

			if tt.want != nil && got == nil {
				t.Errorf("extractDateFromIndex() = nil, want %v", tt.want)
				return
			}

			if tt.want != nil && got != nil {
				if !got.Equal(*tt.want) {
					t.Errorf("extractDateFromIndex() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestIndexInDateRange(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name      string
		indexName string
		fromTime  *time.Time
		toTime    *time.Time
		want      bool
	}{
		{
			name:      "index within range",
			indexName: "logstash-2024.01.15",
			fromTime:  timePtr(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)),
			toTime:    timePtr(time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)),
			want:      true,
		},
		{
			name:      "index before range",
			indexName: "logstash-2024.01.05",
			fromTime:  timePtr(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)),
			toTime:    timePtr(time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)),
			want:      false,
		},
		{
			name:      "index after range",
			indexName: "logstash-2024.01.25",
			fromTime:  timePtr(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)),
			toTime:    timePtr(time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)),
			want:      false,
		},
		{
			name:      "no from time",
			indexName: "logstash-2024.01.05",
			fromTime:  nil,
			toTime:    timePtr(time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)),
			want:      true,
		},
		{
			name:      "no to time",
			indexName: "logstash-2024.01.25",
			fromTime:  timePtr(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)),
			toTime:    nil,
			want:      true,
		},
		{
			name:      "no date constraints",
			indexName: "logstash-2024.01.15",
			fromTime:  nil,
			toTime:    nil,
			want:      true,
		},
		{
			name:      "index without date pattern",
			indexName: "system-logs",
			fromTime:  timePtr(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)),
			toTime:    timePtr(time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)),
			want:      true, // Includes indices without date patterns
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.indexInDateRange(tt.indexName, tt.fromTime, tt.toTime)
			if err != nil && tt.indexName != "system-logs" {
				t.Errorf("indexInDateRange() error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("indexInDateRange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractDateString(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name      string
		indexName string
		startIdx  int
		pattern   string
		want      string
	}{
		{
			name:      "extract dot separated date",
			indexName: "logstash-2024.01.15",
			startIdx:  9, // position of "2024"
			pattern:   "2006.01.02",
			want:      "2024.01.15",
		},
		{
			name:      "extract dash separated date",
			indexName: "filebeat-2024-01-15",
			startIdx:  9, // position of "2024"
			pattern:   "2006-01-02",
			want:      "2024-01-15",
		},
		{
			name:      "extract month only",
			indexName: "logs-2024.01",
			startIdx:  5, // position of "2024"
			pattern:   "2006.01",
			want:      "2024.01",
		},
		{
			name:      "extract compact date",
			indexName: "logs20240115",
			startIdx:  4, // position of "20240115"
			pattern:   "20060102",
			want:      "20240115",
		},
		{
			name:      "invalid pattern",
			indexName: "logstash-2024.01.15",
			startIdx:  9,
			pattern:   "2006-01-02", // Wrong separator
			want:      "",
		},
		{
			name:      "start index out of bounds",
			indexName: "logs-2024.01",
			startIdx:  20,
			pattern:   "2006.01.02",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.extractDateString(tt.indexName, tt.startIdx, tt.pattern)
			if got != tt.want {
				t.Errorf("extractDateString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDateRange(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		{
			name:    "valid date range",
			from:    "2024-01-01",
			to:      "2024-01-31",
			wantErr: false,
		},
		{
			name:    "date math range",
			from:    "now-7d",
			to:      "now",
			wantErr: false,
		},
		{
			name:    "empty range",
			from:    "",
			to:      "",
			wantErr: false,
		},
		{
			name:    "only from date",
			from:    "2024-01-01",
			to:      "",
			wantErr: false,
		},
		{
			name:    "only to date",
			from:    "",
			to:      "2024-01-31",
			wantErr: false,
		},
		{
			name:    "invalid from date",
			from:    "invalid-date",
			to:      "2024-01-31",
			wantErr: true,
		},
		{
			name:    "invalid to date",
			from:    "2024-01-01",
			to:      "invalid-date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fromTime, toTime, err := client.parseDateRange(tt.from, tt.to)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDateRange() expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parseDateRange() error = %v", err)
				return
			}

			// Check that times are set correctly
			if tt.from != "" && fromTime == nil {
				t.Errorf("parseDateRange() fromTime is nil when from='%s'", tt.from)
			}

			if tt.to != "" && toTime == nil {
				t.Errorf("parseDateRange() toTime is nil when to='%s'", tt.to)
			}

			if tt.from == "" && fromTime != nil {
				t.Errorf("parseDateRange() fromTime should be nil when from is empty")
			}

			if tt.to == "" && toTime != nil {
				t.Errorf("parseDateRange() toTime should be nil when to is empty")
			}
		})
	}
}

// Helper function to create time pointers
func timePtr(t time.Time) *time.Time {
	return &t
}
