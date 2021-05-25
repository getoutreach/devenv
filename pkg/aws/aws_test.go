package aws

import "testing"

func Test_assumedToRole(t *testing.T) {
	type args struct {
		assumedRole string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "should properly parse principal_arn",
			args: args{
				assumedRole: "arn:aws:sts::182192988802:assumed-role/okta_eng_readonly_role/jared.allard@outreach.io",
			},
			want: "arn:aws:iam::182192988802:role/okta_eng_readonly_role",
		},
		{
			name: "should ignore invalid input",
			args: args{
				assumedRole: "hello world",
			},
			want: "hello world",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := assumedToRole(tt.args.assumedRole); got != tt.want { //nolint:scopelint
				t.Errorf("assumedToRole() = %v, want %v", got, tt.want) //nolint:scopelint
			}
		})
	}
}
