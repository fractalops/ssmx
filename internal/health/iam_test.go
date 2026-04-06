package health

import "testing"

func TestInstanceProfileName(t *testing.T) {
	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:iam::123456789012:instance-profile/MyProfile", "MyProfile"},
		{"arn:aws:iam::123456789012:instance-profile/path/to/MyProfile", "MyProfile"},
		{"MyProfile", "MyProfile"}, // no slash — returned as-is
	}
	for _, tc := range cases {
		if got := instanceProfileName(tc.arn); got != tc.want {
			t.Errorf("instanceProfileName(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}
