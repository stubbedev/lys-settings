---
layout: home

hero:
  name: lys-settings
  text: Settings for the lys desktop
  tagline: One window for lys-shell and lys-wm. It reads each project's own config rules and never keeps a copy of them.
  image:
    src: /lys-settings.svg
    alt: lys-settings
  actions:
    - theme: brand
      text: Get started
      link: /guide/getting-started
    - theme: alt
      text: Install
      link: /guide/install
    - theme: alt
      text: GitHub
      link: https://github.com/stubbedev/lys-settings

features:
  - title: Your config stays yours
    details: Every change is a runtime override above the config.yaml you declare, which is only ever read. A lys-wm override replaces only the bytes of its value, and an edit that would change more than its key is refused.
  - title: Checked before it is written
    details: lys-wm's own parser checks every change before the override file is replaced, and a running lys-wm reloads it after. A refused change leaves the file untouched and says why.
  - title: Works with Home Manager
    details: A config that is a read-only link into the Nix store keeps working. Changes land as overrides you can reset per setting or all at once.
  - title: Tested on a real lys-wm
    details: The end-to-end test starts a headless lys-wm in a private runtime directory, changes its config through the app's code, reads the result back over IPC, and opens the real window on it.
---
