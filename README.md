# hugo-multiversion

This is a really simple CLI tool that makes managing multi-version Hugo sites
easier.

It clones a repository containing content at multiple different revisions, and
assembles an 'output' content directory containing all the versioned content.

This allows you to maintain documentation/content for different versions in
different branches, allowing you to easily cherry pick changes around.

## Usage



```
go run . \
    --repo-url https://github.com/cert-manager/docs.git \
    --repo-content-dir docs/
    --output-dir output/ \
    --latest-branch=release-0.12 \
    --branches v0.12=release-0.12,v0.11=release-0.11 \
```
