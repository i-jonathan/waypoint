package servertest

import "testing"

func TestSkipsWithoutEnv(t *testing.T) {
	t.Setenv("WAYPOINT_TEST_POSTGRES", "")
	if _, ok, err := lookup("WAYPOINT_TEST_POSTGRES"); ok || err != nil {
		t.Fatalf("unset variable: ok=%v err=%v, want not ok and no error", ok, err)
	}
}

func TestParsesConnection(t *testing.T) {
	t.Setenv("WAYPOINT_TEST_POSTGRES", "127.0.0.1:5999:waypoint:pa:ss:word")
	c, ok, err := lookup("WAYPOINT_TEST_POSTGRES")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if c.host != "127.0.0.1" || c.port != 5999 || c.user != "waypoint" || c.password != "pa:ss:word" {
		t.Fatalf("parsed %+v", c)
	}
	t.Setenv("WAYPOINT_TEST_POSTGRES", "nonsense")
	if _, _, err := lookup("WAYPOINT_TEST_POSTGRES"); err == nil {
		t.Fatal("malformed value should be an error")
	}
}

func TestDatabaseNames(t *testing.T) {
	a, b := dbName(), dbName()
	if a == b || len(a) != len("waypoint_test_")+8 {
		t.Fatalf("names %q %q", a, b)
	}
}
