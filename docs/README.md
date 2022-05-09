# devenv Docs

Documentation site for [devenv](https://github.com/getoutreach/devenv). Viewable at <https://engineering.outreach.io/devenv>

## Contributing

We welcome contributions to devenv of any kind including documentation, suggestions, bug reports, pull requests etc.

Note that this folder contains solely the documentation for devenv. For contributions that aren't documentation-related please refer to the root of the repository.

Spelling fixes are most welcomed, and if you want to contribute longer sections to the documentation, it would be great if you had the following criteria in mind when writing:

- Short is good. People go to the library to read novels. If there is more than one way to _do a thing_ in devenv, describe the current _best practice_ (avoid "… but you can also do …" and "… in older versions of devenv you had to …".
- For example, try to find short snippets that teaches people about the concept. If the example is also useful as-is (copy and paste), then great. Don't list long and similar examples in the documentation, we want to keep this as easy to consume as possible.
- We want to be friendly towards users across that world, so easy to understand and [simple English](https://simple.wikipedia.org/wiki/Basic_English) is good.

## Branches

- The `main` branch is where the site is automatically built from, and is the place to put changes relevant to the current Hugo version.

## Build

To view the documentation site locally, you need to clone this repository:

```bash
git clone https://github.com/getoutreach/devenv.git
cd docs/
```

Then to view the docs in your browser use npm:

```bash
▶ npm run start
```

Then you should be able to access the docs at http://localhost:1313/devenv
