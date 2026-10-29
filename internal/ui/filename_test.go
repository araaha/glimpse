package ui

import "testing"

func TestFormatFilename(t *testing.T) {
	const home = "/home/user"
	for _, tt := range []struct {
		path, full, short, tail string
	}{
		{"/home/user/.config/neovim/plugins/init.lua", "~/.config/neovim/plugins/init.lua", "~/.c/n/p/init.lua", "init.lua"},
		{"/home/user/notes.txt", "~/notes.txt", "~/notes.txt", "notes.txt"},
		{"/home/userx/f.txt", "/home/userx/f.txt", "/h/u/f.txt", "f.txt"},
		{"/etc/ssh/sshd_config", "/etc/ssh/sshd_config", "/e/s/sshd_config", "sshd_config"},
		{"/hosts", "/hosts", "/hosts", "hosts"},
		{"/home/user/日記/.ünï/a.md", "~/日記/.ünï/a.md", "~/日/.ü/a.md", "a.md"},
	} {
		for style, want := range map[string]string{FilenameFull: tt.full, "": tt.full, FilenameShort: tt.short, FilenameTail: tt.tail} {
			if got := formatFilename(tt.path, home, style); got != want {
				t.Errorf("%s %q = %q, want %q", style, tt.path, got, want)
			}
		}
	}
	for _, home := range []string{"", "/"} {
		if got := formatFilename("/a/b.txt", home, FilenameFull); got != "/a/b.txt" {
			t.Errorf("home %q: %q", home, got)
		}
	}
	if got := formatFilename("/home/user/x", home+"/", FilenameFull); got != "~/x" {
		t.Errorf("trailing slash home: %q", got)
	}
}

func TestFormatFilenameResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Chdir(dir)
	if got := FormatFilename("sub/file.txt", FilenameFull); got != "~/sub/file.txt" {
		t.Fatalf("relative path = %q", got)
	}
}

func TestStatuslineClipsPathsFromTheLeft(t *testing.T) {
	s, err := NewStatusline(StatuslineConfig{Left: []string{StatusFilename}, Right: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path bool
		want string
	}{{true, " …m/init.lua "}, {false, " ~/.config/… "}} {
		bar := s.Format(StatusState{Filename: "~/.config/nvim/init.lua", FilenamePath: tt.path}, 13, false)
		if got := bar.Text(); got != tt.want {
			t.Errorf("path %v: %q, want %q", tt.path, got, tt.want)
		}
	}
}
