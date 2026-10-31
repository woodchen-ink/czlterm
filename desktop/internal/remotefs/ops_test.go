package remotefs

import "testing"

func TestCopyNumbersDuplicates(t *testing.T) {
	c := memClient(t)
	if err := WriteFile(c, "/a.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/a (1).txt", "/a (2).txt"} {
		got, err := Copy(c, "/a.txt", "/")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if body := read(t, c, got); body != "one" {
			t.Fatalf("copy content %q", body)
		}
	}
}

func TestCopyTree(t *testing.T) {
	c := memClient(t)
	for _, d := range []string{"/src", "/src/sub", "/dst"} {
		if err := c.Mkdir(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteFile(c, "/src/sub/f", []byte("x")); err != nil {
		t.Fatal(err)
	}
	got, err := Copy(c, "/src", "/dst")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/dst/src" || read(t, c, "/dst/src/sub/f") != "x" {
		t.Fatalf("tree not copied: %q", got)
	}
	if _, err := Copy(c, "/src", "/src/sub"); err == nil {
		t.Fatal("copy into itself accepted")
	}
}

func TestMove(t *testing.T) {
	c := memClient(t)
	if err := c.Mkdir("/dst"); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(c, "/a", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(c, "/dst/b", []byte("2")); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(c, "/a", "/dst"); err != nil {
		t.Fatal(err)
	}
	if read(t, c, "/dst/a") != "1" {
		t.Fatal("not moved")
	}
	if err := WriteFile(c, "/b", []byte("3")); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(c, "/b", "/dst"); err == nil {
		t.Fatal("move overwrote existing target")
	}
	if read(t, c, "/dst/b") != "2" {
		t.Fatal("existing target changed")
	}
}

func TestChmodRejectsBadInput(t *testing.T) {
	c := memClient(t)
	if err := Chmod(c, "/", 0o10000, ChmodSelf); err == nil {
		t.Fatal("mode above 07777 accepted")
	}
	if err := Chmod(c, "/", 0o755, "bogus"); err == nil {
		t.Fatal("unknown scope accepted")
	}
}
