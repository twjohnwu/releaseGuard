package configdrift

import "testing"

func TestClassifyConfigChange(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"config/secrets.yaml", "critical"},
		{"helm/values.yaml", "medium"},
		{"k8s/deployment.yaml", "high"},
		{"config/feature_flags.yaml", "medium"},
		{"foo.txt", "low"},
	}
	for _, c := range cases {
		got := classify(c.path)
		if got != c.want {
			t.Errorf("classify(%q) = %s, want %s", c.path, got, c.want)
		}
	}
}
