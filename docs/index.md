---
outline: deep
---

# Introduction

ingestr is a command-line app that allows you to ingest data from any source into any destination using simple command-line flags, no code necessary.

- ✨ copy data from your database into any destination
- ➕ incremental loading: `append`, `merge` or `delete+insert`
- 🐍 single-command installation

ingestr takes away the complexity of managing any backend or writing any code for ingesting data, simply run the command and watch the data land on its destination.

## Installation

We recommend using [uv](https://github.com/astral-sh/uv) to run `ingestr`.

```
pip install uv
uvx ingestr
```

Alternatively, if you'd like to install it globally:
```
uv pip install --system ingestr
```

While installation with vanilla `pip` is possible, it's an order of magnitude slower.

## Next Steps

Check out the [Quickstart](/getting-started/quickstart.md) guide to get started with ingestr.

## Community

Join our Slack community [here](https://join.slack.com/t/bruindatacommunity/shared_invite/zt-2dl2i8foy-bVsuMUauHeN9M2laVm3ZVg).

## License

This project is licensed under the MIT License - see the [LICENSE](https://github.com/bruin-data/ingestr/blob/main/LICENSE.md) file for details.

Some components are licensed under Apache 2.0 - see the [NOTICE](https://github.com/bruin-data/ingestr/blob/main/NOTICE) file for details.