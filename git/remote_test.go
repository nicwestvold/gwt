package git

import "testing"

func TestParseCanonicalName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"https://github.com/grafana/metrics-drilldown", "grafana/metrics-drilldown"},
		{"https://github.com/grafana/metrics-drilldown/", "grafana/metrics-drilldown"},
		{"https://github.com/grafana/metrics-drilldown.git/", "grafana/metrics-drilldown"},
		{"git@github.com:grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"git@github.com:grafana/metrics-drilldown", "grafana/metrics-drilldown"},
		{"ssh://git@github.com/grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"ssh://git@github.com/grafana/metrics-drilldown", "grafana/metrics-drilldown"},
		{"https://gitlab.com/org/subgroup/repo.git", "subgroup/repo"},
		{"git@gitlab.com:org/subgroup/repo.git", "subgroup/repo"},
		{"/path/to/repo", "to/repo"},
		{"repo", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := ParseCanonicalName(tt.url)
			if got != tt.want {
				t.Errorf("ParseCanonicalName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
