package generate

import "testing"

func TestExtractJSONArray(t *testing.T) {
	arr := `[{"situation":"s","question":"q"}]`
	cases := map[string]string{
		arr: arr,
		"Looking at this batch, let me work through:\n\n" + arr: arr,
		"```json\n" + arr + "\n```":                             arr,
		"prose with a stray [bracket] then the real:\n" + arr:   arr,
	}
	for in, want := range cases {
		if got := extractJSONArray(in); got != want {
			t.Errorf("extractJSONArray(%.40q...) = %q, want %q", in, got, want)
		}
	}
	// unrecoverable input returns unchanged (caller error path fires)
	if got := extractJSONArray("no json here"); got != "no json here" {
		t.Errorf("unrecoverable: got %q", got)
	}
}
