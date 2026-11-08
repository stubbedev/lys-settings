---
layout: home

hero:
  name: lys-settings
  text: Settings for the lys desktop
  tagline: One window for lys-shell, lys-wm and the system services they use.
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
    details: Changes are runtime overrides over the config.yaml you declare, which is only read. A lys-wm edit changes only the bytes of its value.
  - title: Checked before it is written
    details: lys-wm's own parser checks every change before the override file is replaced, and a running lys-wm reloads it. A refused change leaves the file untouched and shows why.
  - title: Works with Home Manager
    details: A config linked read-only into the Nix store stays editable through overrides.
  - title: No copied rules
    details: Fields, defaults and validation come from lys-shell's Go packages and the installed lys-wm.
---
