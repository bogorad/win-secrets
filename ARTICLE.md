# /run/secrets in win11? yes!

## How to use your SOPS secrets like you're in linux :)

It all started with me wishing for a single OpenCode config on all platforms - win11 on both amd64 and arm64, as well as nixos (amd64/arm64). No big deal, I'll use my `.dotfiles`! But properly securing secrets is no small thing. In nixos, I heavily rely on SOPS-nix, which exports secrets as `/run/secrets`, while on win11 there is (was) no such mechanism. I considered creating a ramdisk, mounting it on NTFS as /run and unpacking secrets via `sops.exe` (available via `scoop`), but that's too much hassle and moving parts. I ended up with two separate configs, the nixos-one with

```json
      "options": {
        "apiKey": "{file:/run/secrets/api_keys/openrouter}"
      },
```

and the win11-one with `"{env:OPENROUTER_API_KEY}"`, and it sucked.

So I started talking to an LLM about my problem. How about I write a program in golang that will mount `/run` and interpret/map path access to actual keys in SOPS? Easier than I expected! `WinFS` was already present (`rclone` uses it), so all I had to do was to come up with an architecture and guide the LLM to write the code.

My current method is - plan via chat with a deep-thinking model (previously `Gemini-2.5-pro`, now `Claude` or `GPT5`, both -thinking). When I'm satisfied with the model's understanding of the task, I tell it to create an extremely detailed TDD for a really slow junior programmer, with explanations, specific places to use/fix the code, and a lot of code snippets in the target language.

Then I read it, make corrections, and finally paste it to a fast-but-dumb model - `grok-code-fast1` is my current favorite (BTW, it's free in OpenCode right now). Usually, it does it in 2-3 tries (it _is_ dumb).

My program works like this. `win-secrets.exe` is started via Task Scheduler on user login. It uses a remote `sops-keyservice` to return decoded per-item keys, thus eliminating the need to have any sensitive files on my local win11 machine, only the encrypted `secrets.yaml` and access to a `sops-keyservice` instance.

So when I type `dir /run/secrets/my/test-password` the program maps the path to `[my][demo-password]`. In the `secrets.yaml` it looks like:

```yaml
my:
  demo-password: xxx
```

My `sops-keyservice` is actually a nixos-running LXC, under proxmox. It uses just 90 MiB of RAM. Here's the flake:

```nix
# lxc/config-sops-keyservice.nix

{
  pkgs,
  ...
}:

{
  networking.hostName = "sops-keyservice";

  imports = [
    ./lxc.nix
  ];

  systemd.services.sops-keyservice = {
    description = "SOPS Key Service";
    after = [ "network.target" ];
    wantedBy = [ "multi-user.target" ];

    serviceConfig = {
      Type = "simple";
      ExecStart = "${pkgs.sops}/bin/sops keyservice --network tcp --address 0.0.0.0:5000 --verbose";
      Restart = "on-failure";
      RestartSec = "5s";
      User = "chuck";
      Environment = "SOPS_AGE_KEY_FILE=/home/chuck/.config/sops/age/keys.txt"; # Set environment for age key location
    };
  };
}
```

Here's [the github link](https://github.com/bogorad/win-secrets), README.md there has a proper explanation.
