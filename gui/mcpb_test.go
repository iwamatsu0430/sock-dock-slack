package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMCPBVersion(t *testing.T) {
	for in, want := range map[string]string{"v0.2.0": "0.2.0", "1.3.4-rc.1": "1.3.4-rc.1", "b2ebb3c-dirty": "0.0.0-b2ebb3c-dirty", "dev": "0.0.0-dev", "": "0.0.0-dev"} {
		if got := mcpbVersion(in); got != want {
			t.Errorf("mcpbVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMCPB(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "sockdock")
	os.WriteFile(cli, []byte("#!/bin/sh\necho fake\n"), 0o755)
	out := filepath.Join(dir, "SockDock.mcpb")
	if err := buildMCPB(context.Background(), out, cli, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	bin, ok := files["server/sockdock"]
	if !ok || bin.Mode().Perm()&0o111 == 0 {
		t.Fatalf("server/sockdock missing or not executable: %v", files)
	}
	rc, _ := files["manifest.json"].Open()
	raw, _ := io.ReadAll(rc)
	rc.Close()
	var man mcpbManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	if man.ManifestVersion == "" || man.Name != "sockdock" || man.Version != "1.2.3" || man.Description == "" || man.Author["name"] == "" {
		t.Errorf("required fields: %+v", man)
	}
	if man.Server.Type != "binary" || man.Server.EntryPoint != "server/sockdock" ||
		man.Server.MCPConfig.Command != "${__dirname}/server/sockdock" || len(man.Server.MCPConfig.Args) != 1 || man.Server.MCPConfig.Args[0] != "mcp" {
		t.Errorf("server: %+v", man.Server)
	}
	if _, ok := files["icon.png"]; !ok || man.Icon != "icon.png" {
		t.Errorf("icon: manifest %q, files %v", man.Icon, files)
	}
	if len(man.Tools) != 13 {
		t.Errorf("tools = %d, want 13", len(man.Tools))
	}
}
