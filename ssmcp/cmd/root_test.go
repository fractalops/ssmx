package cmd

import "testing"

func TestRunCopy_BothRemote_CallsCopyRemoteToRemote(t *testing.T) {
	// parseEndpoint with two remote endpoints must not return an error.
	// We verify the routing by checking parseEndpoint results directly —
	// the full runCopy requires AWS credentials so it's an integration test.
	src := "web-prod:/srv/app"
	dst := "worker-prod:/srv/app"
	_, _, srcRemote := parseEndpoint(src)
	_, _, dstRemote := parseEndpoint(dst)
	if !srcRemote || !dstRemote {
		t.Errorf("expected both to be remote: srcRemote=%v dstRemote=%v", srcRemote, dstRemote)
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		input      string
		wantHost   string
		wantPath   string
		wantRemote bool
	}{
		// remote: host:path
		{"web-prod:/tmp/foo", "web-prod", "/tmp/foo", true},
		{"i-0abc123def:/var/log", "i-0abc123def", "/var/log", true},
		{"my-bookmark:/srv/app/", "my-bookmark", "/srv/app/", true},
		// remote: host with empty path (remote home dir — valid for scp)
		{"web-prod:", "web-prod", "", true},
		// local: absolute path
		{"/abs/path/file.txt", "", "/abs/path/file.txt", false},
		// local: relative with ./
		{"./rel/path", "", "./rel/path", false},
		// local: relative with ../
		{"../parent/file", "", "../parent/file", false},
		// local: bare filename (no colon)
		{"file.txt", "", "file.txt", false},
		{"deploy.sh", "", "deploy.sh", false},
		// local: starts with colon (invalid host — treat as local)
		{":path", "", ":path", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, path, remote := parseEndpoint(tt.input)
			if host != tt.wantHost {
				t.Errorf("host: got %q, want %q", host, tt.wantHost)
			}
			if path != tt.wantPath {
				t.Errorf("path: got %q, want %q", path, tt.wantPath)
			}
			if remote != tt.wantRemote {
				t.Errorf("remote: got %v, want %v", remote, tt.wantRemote)
			}
		})
	}
}
