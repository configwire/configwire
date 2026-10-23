package identity

import "testing"

// TestParseBucketParam_Parity pins the single canonical ?b= rule shared by
// fetch.ParseBucket and ingest validation: C1-C3 canonical, M1-M4
// malformed (byte-identical 400 text), A1 absent.
func TestParseBucketParam_Parity(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "C1/zero-valid", raw: "0", want: "0"},
		{name: "C2/mid-valid", raw: "42", want: "42"},
		{name: "C3/max-valid", raw: "9999", want: "9999"},
		{name: "M1/leading-zero", raw: "007", wantErr: ErrInvalidBucketMessage},
		{name: "M2/double-zero", raw: "00", wantErr: ErrInvalidBucketMessage},
		{name: "M3/whitespace-padded", raw: " 12 ", wantErr: ErrInvalidBucketMessage},
		{name: "M4/out-of-range", raw: "10000", wantErr: ErrInvalidBucketMessage},
		{name: "A1/absent", raw: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBucketParam(tc.raw)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseBucketParam(%q) = (%q, nil), want error %q", tc.raw, got, tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("ParseBucketParam(%q) error = %q, want %q", tc.raw, err.Error(), tc.wantErr)
				}
				if got != "" {
					t.Fatalf("ParseBucketParam(%q) value = %q, want empty on error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBucketParam(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("ParseBucketParam(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestIsCanonicalBucket_Parity pins the predicate behind ParseBucketParam.
func TestIsCanonicalBucket_Parity(t *testing.T) {
	valid := []string{"0", "42", "9999"}
	for _, s := range valid {
		if !IsCanonicalBucket(s) {
			t.Errorf("IsCanonicalBucket(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "007", "00", " 12 ", "10000", "-1", "12.0"}
	for _, s := range invalid {
		if IsCanonicalBucket(s) {
			t.Errorf("IsCanonicalBucket(%q) = true, want false", s)
		}
	}
}
