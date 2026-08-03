package panel

import "testing"

func TestParseUserInfo(t *testing.T) {
	cases := []struct {
		name string
		in   string
		port int
		ent  string
		user string
	}{
		{
			name: "en",
			in: "Panel address: http://$LOCAL_IP:62045/tomas \n" +
				"user: ankio\nUser password: ********\n",
			port: 62045,
			ent:  "tomas",
			user: "ankio",
		},
		{
			name: "zh",
			in: "面板地址: http://$LOCAL_IP:52045/sec \n" +
				"用户: admin\n用户密码: ********\n",
			port: 52045,
			ent:  "sec",
			user: "admin",
		},
		{
			name: "ansi",
			in:   "\x1b[0;34mPanel address: http://127.0.0.1:8080/abc\x1b[0m\nUser: u1\n",
			port: 8080,
			ent:  "abc",
			user: "u1",
		},
	}
	for _, tc := range cases {
		st, err := parseUserInfo([]byte(tc.in))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if st.ServerPort != tc.port || st.SecurityEntrance != tc.ent || st.UserName != tc.user {
			t.Fatalf("%s: got %+v", tc.name, st)
		}
	}
}

func TestParseUserInfoMissingPort(t *testing.T) {
	if _, err := parseUserInfo([]byte("user: ankio\n")); err == nil {
		t.Fatal("expected error")
	}
}
