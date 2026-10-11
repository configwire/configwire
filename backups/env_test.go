package backups

import (
	"os"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		storedCron  string
		storedKeep  int
		cronEnv     *string
		keepEnv     *string
		wantCron    string
		wantKeep    int
		wantChanged bool
	}{
		{
			name:        "no env leaves stored values untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     nil,
			keepEnv:     nil,
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "empty cron env leaves stored cron untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     strPtr(""),
			keepEnv:     nil,
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "whitespace cron env leaves stored cron untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     strPtr("   "),
			keepEnv:     nil,
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "valid cron only applies cron and keeps stored keep",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     strPtr("0 4 * * *"),
			keepEnv:     nil,
			wantCron:    "0 4 * * *",
			wantKeep:    7,
			wantChanged: true,
		},
		{
			name:        "valid cron and valid keep both applied",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     strPtr("0 5 * * *"),
			keepEnv:     strPtr("14"),
			wantCron:    "0 5 * * *",
			wantKeep:    14,
			wantChanged: true,
		},
		{
			name:        "non-numeric keep leaves stored keep untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     nil,
			keepEnv:     strPtr("not-a-number"),
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "zero keep leaves stored keep untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     nil,
			keepEnv:     strPtr("0"),
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "negative keep leaves stored keep untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     nil,
			keepEnv:     strPtr("-3"),
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "cron newly set with stored keep zero gets default keep",
			storedCron:  "",
			storedKeep:  0,
			cronEnv:     strPtr("0 3 * * *"),
			keepEnv:     nil,
			wantCron:    "0 3 * * *",
			wantKeep:    DefaultCronMaxKeep,
			wantChanged: true,
		},
		{
			name:        "cron newly set with valid env keep uses env keep",
			storedCron:  "",
			storedKeep:  0,
			cronEnv:     strPtr("0 3 * * *"),
			keepEnv:     strPtr("5"),
			wantCron:    "0 3 * * *",
			wantKeep:    5,
			wantChanged: true,
		},
		{
			name:        "cron newly set keeps stored keep when already valid",
			storedCron:  "",
			storedKeep:  7,
			cronEnv:     strPtr("0 3 * * *"),
			keepEnv:     nil,
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: true,
		},
		{
			name:        "idempotent second boot with same env yields unchanged",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     strPtr("0 3 * * *"),
			keepEnv:     strPtr("7"),
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
		{
			name:        "keep-only change applies",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     nil,
			keepEnv:     strPtr("10"),
			wantCron:    "0 3 * * *",
			wantKeep:    10,
			wantChanged: true,
		},
		{
			name:        "keep env whitespace leaves stored keep untouched",
			storedCron:  "0 3 * * *",
			storedKeep:  7,
			cronEnv:     nil,
			keepEnv:     strPtr("   "),
			wantCron:    "0 3 * * *",
			wantKeep:    7,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cronEnv != nil {
				t.Setenv(CronEnvVar, *tt.cronEnv)
			} else {
				mustUnset(t, CronEnvVar)
			}
			if tt.keepEnv != nil {
				t.Setenv(KeepEnvVar, *tt.keepEnv)
			} else {
				mustUnset(t, KeepEnvVar)
			}

			gotCron, gotKeep, gotChanged, logMsg := Resolve(tt.storedCron, tt.storedKeep)

			if gotCron != tt.wantCron {
				t.Fatalf("cron: got %q, want %q", gotCron, tt.wantCron)
			}
			if gotKeep != tt.wantKeep {
				t.Fatalf("keep: got %d, want %d", gotKeep, tt.wantKeep)
			}
			if gotChanged != tt.wantChanged {
				t.Fatalf("changed: got %v, want %v", gotChanged, tt.wantChanged)
			}
			if !strings.HasPrefix(logMsg, "configwire: backups ") {
				t.Fatalf("log message must use the configwire: backups prefix, got %q", logMsg)
			}
		})
	}
}

// TestResolveValidationPin pins the PocketBase BackupsConfig.Validate
// interaction: CronMaxKeep >= 1 is required whenever Cron != "".
// The resolver must therefore never output ("non-empty cron", keep < 1).
func TestResolveValidationPin(t *testing.T) {
	t.Setenv(CronEnvVar, "0 3 * * *")
	mustUnset(t, KeepEnvVar)

	gotCron, gotKeep, _, _ := Resolve("", 0)
	if gotCron == "" {
		t.Fatalf("expected cron to be set from env, got empty")
	}
	if gotKeep < 1 {
		t.Fatalf("PB BackupsConfig.Validate requires CronMaxKeep >= 1 when Cron != %q, got keep=%d", "", gotKeep)
	}
}

func strPtr(s string) *string {
	return &s
}

func TestResolveS3(t *testing.T) {
	off := S3{}
	on := S3{
		Enabled:        true,
		Endpoint:       "https://s3.example.com",
		Bucket:         "cw-backups",
		Region:         "us-east-1",
		AccessKey:      "AKID",
		Secret:         "s3cr3t",
		ForcePathStyle: true,
	}
	tests := []struct {
		name        string
		stored      S3
		env         map[string]*string
		want        S3
		wantChanged bool
		wantFields  []string
	}{
		{
			name:        "no env leaves stored s3 untouched",
			stored:      off,
			env:         nil,
			want:        off,
			wantChanged: false,
		},
		{
			name:        "empty env values leave stored s3 untouched",
			stored:      on,
			env:         map[string]*string{S3EnabledEnvVar: strPtr(""), S3BucketEnvVar: strPtr("  ")},
			want:        on,
			wantChanged: false,
		},
		{
			name:   "partial fields apply only those",
			stored: off,
			env: map[string]*string{
				S3EndpointEnvVar: strPtr("https://minio.local:9000"),
				S3BucketEnvVar:   strPtr("cw-backups"),
			},
			want:        S3{Endpoint: "https://minio.local:9000", Bucket: "cw-backups"},
			wantChanged: true,
			wantFields:  []string{"endpoint", "bucket"},
		},
		{
			name:        "secret-only change applies without logging values",
			stored:      S3{Secret: "old"},
			env:         map[string]*string{S3SecretEnvVar: strPtr("new")},
			want:        S3{Secret: "new"},
			wantChanged: true,
			wantFields:  []string{"secret"},
		},
		{
			name:        "invalid enabled bool is ignored fail-soft",
			stored:      off,
			env:         map[string]*string{S3EnabledEnvVar: strPtr("maybe")},
			want:        off,
			wantChanged: false,
		},
		{
			name:        "invalid force-path-style bool is ignored fail-soft",
			stored:      off,
			env:         map[string]*string{S3ForcePathStyleEnvVar: strPtr("sometimes")},
			want:        off,
			wantChanged: false,
		},
		{
			name:   "falsy enabled disables stored s3",
			stored: on,
			env:    map[string]*string{S3EnabledEnvVar: strPtr("false")},
			want: S3{
				Enabled:        false,
				Endpoint:       on.Endpoint,
				Bucket:         on.Bucket,
				Region:         on.Region,
				AccessKey:      on.AccessKey,
				Secret:         on.Secret,
				ForcePathStyle: on.ForcePathStyle,
			},
			wantChanged: true,
			wantFields:  []string{"enabled"},
		},
		{
			name:   "full set applies every field",
			stored: off,
			env: map[string]*string{
				S3EnabledEnvVar:        strPtr("1"),
				S3EndpointEnvVar:       strPtr("https://s3.example.com"),
				S3BucketEnvVar:         strPtr("cw-backups"),
				S3RegionEnvVar:         strPtr("us-east-1"),
				S3AccessKeyEnvVar:      strPtr("AKID"),
				S3SecretEnvVar:         strPtr("s3cr3t"),
				S3ForcePathStyleEnvVar: strPtr("yes"),
			},
			want:        on,
			wantChanged: true,
			wantFields:  []string{"endpoint", "bucket", "region", "access-key", "secret", "force-path-style", "enabled"},
		},
		{
			name:   "idempotent second boot with same env yields unchanged",
			stored: on,
			env: map[string]*string{
				S3EnabledEnvVar:        strPtr("true"),
				S3EndpointEnvVar:       strPtr("https://s3.example.com"),
				S3BucketEnvVar:         strPtr("cw-backups"),
				S3RegionEnvVar:         strPtr("us-east-1"),
				S3AccessKeyEnvVar:      strPtr("AKID"),
				S3SecretEnvVar:         strPtr("s3cr3t"),
				S3ForcePathStyleEnvVar: strPtr("1"),
			},
			want:        on,
			wantChanged: false,
		},
	}

	vars := []string{
		S3EnabledEnvVar, S3EndpointEnvVar, S3BucketEnvVar, S3RegionEnvVar,
		S3AccessKeyEnvVar, S3SecretEnvVar, S3ForcePathStyleEnvVar,
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, v := range vars {
				mustUnset(t, v)
			}
			for k, v := range tt.env {
				if v != nil {
					t.Setenv(k, *v)
				}
			}

			got, gotChanged, logMsg := ResolveS3(tt.stored)

			if got != tt.want {
				t.Fatalf("s3: got %+v, want %+v", got, tt.want)
			}
			if gotChanged != tt.wantChanged {
				t.Fatalf("changed: got %v, want %v", gotChanged, tt.wantChanged)
			}
			if !strings.HasPrefix(logMsg, "configwire: backups ") {
				t.Fatalf("log message must use the configwire: backups prefix, got %q", logMsg)
			}
			for _, f := range tt.wantFields {
				if !strings.Contains(logMsg, f) {
					t.Fatalf("log message must name field %q, got %q", f, logMsg)
				}
			}
			for _, secret := range []string{got.Endpoint, got.Bucket, got.Region, got.AccessKey, got.Secret} {
				if secret != "" && strings.Contains(logMsg, secret) {
					t.Fatalf("log message must never contain s3 field values, got %q", logMsg)
				}
			}
		})
	}
}

func mustUnset(t *testing.T, name string) {
	t.Helper()
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unsetenv %s: %v", name, err)
	}
}
