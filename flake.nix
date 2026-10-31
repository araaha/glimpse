{
  description = "A terminal pager for files and streams";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      # glimpse is Unix-only.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.glimpse;

        # go.mod requires Go 1.27, newer than nixpkgs' default Go.
        glimpse = pkgs.buildGo127Module rec {
          pname = "glimpse";
          version = self.shortRev or self.dirtyShortRev or "dev";
          src = self;

          # Update when go.mod or go.sum change: set it to pkgs.lib.fakeHash and
          # use the hash that nix build reports.
          vendorHash = "sha256-2mpX5G6xpoomrxfbV5RikxJbpIYWoIDtUXhM/zrExtw=";

          subPackages = [ "cmd/glimpse" ];
          ldflags = [
            "-s"
            "-w"
            "-X github.com/araaha/glimpse/cmd/glimpse/command.Version=${version}"
          ];

          nativeBuildInputs = [ pkgs.installShellFiles ];
          postInstall = ''
            installManPage docs/glimpse.1
            installShellCompletion --cmd glimpse \
              --bash <($out/bin/glimpse --completion bash) \
              --zsh <($out/bin/glimpse --completion zsh) \
              --fish <($out/bin/glimpse --completion fish)
            mkdir -p $out/share/nushell/vendor/autoload
            $out/bin/glimpse --completion nushell > $out/share/nushell/vendor/autoload/glimpse.nu
            install -Dm644 glimpse.toml $out/share/doc/glimpse/glimpse.toml
          '';

          meta = {
            description = "Terminal pager for files and streams";
            homepage = "https://github.com/araaha/glimpse";
            license = pkgs.lib.licenses.gpl3Plus;
            mainProgram = "glimpse";
            platforms = pkgs.lib.platforms.unix;
          };
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go_1_27
            pkgs.goreleaser
            pkgs.vhs
          ];
        };
      });
    };
}
