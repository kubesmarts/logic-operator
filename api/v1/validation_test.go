package v1

import (
	"testing"
)

func TestValidateImageFormat(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{
			name:    "simple image with tag",
			image:   "nginx:latest",
			wantErr: false,
		},
		{
			name:    "image with slash and tag",
			image:   "library/nginx:1.21.0",
			wantErr: false,
		},
		{
			name:    "image with domain, subfolder and tag",
			image:   "registry.redhat.io/ubi9/ubi-minimal:latest",
			wantErr: false,
		},
		{
			name:    "image with nested paths and complex tag",
			image:   "quay.io/kubesmarts/flow-runner:v1.0.0-alpha",
			wantErr: false,
		},
		{
			name:    "image with underscores in name",
			image:   "my-repo/my_image:1.2-beta_3",
			wantErr: false,
		},
		{
			name:    "image with dot in tag",
			image:   "nginx:1.2",
			wantErr: false,
		},
		{
			name:    "missing tag",
			image:   "nginx",
			wantErr: true,
		},
		{
			name:    "empty tag",
			image:   "nginx:",
			wantErr: true,
		},
		{
			name:    "missing image name",
			image:   ":latest",
			wantErr: true,
		},
		{
			name:    "multiple colons",
			image:   "nginx::latest",
			wantErr: true,
		},
		{
			name:    "spaces in image name",
			image:   "my repo/my-image:latest",
			wantErr: true,
		},
		{
			name:    "spaces in tag",
			image:   "nginx:latest tag",
			wantErr: true,
		},
		{
			name:    "uppercase in domain/repo path",
			image:   "registry.redhat.io/UBI9/ubi-minimal:latest",
			wantErr: true,
		},
		{
			name:    "colon with port is unsupported by regex",
			image:   "my-registry.example.com:5000/my-image:1.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImageFormat(tt.image)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateImageFormat(%q) error = %v, wantErr %v", tt.image, err, tt.wantErr)
			}
		})
	}
}
