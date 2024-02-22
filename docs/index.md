---
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: "ingestr"
  text: Copy data between any source and any destination
  tagline: "ingestr is a command-line application that allows ingesting or copying data from any source into any destination database."
  image:
    src: https://github.com/bruin-data/ingestr/blob/main/resources/demo.gif?raw=true
    alt: ingestr logo
  actions:
    - theme: brand
      text: Getting Started
      link: /getting-started/quickstart.md
    - theme: alt
      text: GitHub
      link: https://github.com/bruin-data/ingestr

features:
  - title: Single command
    details: ingestr allows copying & ingesting data from any source to any destination with a single command.
  - title: Many sources & destinations
    details: ingestr supports all common source and destination databases.
  - title: Incremental Loading
    details: ingestr supports both full-refresh as well as incremental loading modes.
---

