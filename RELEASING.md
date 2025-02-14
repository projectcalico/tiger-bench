# How to make a release

Here are the steps used to make the v0.1.0 release, as an example to build on (or automate away) for the future.

- Make a release-v0.1 branch.
    git checkout origin/main
    git checkout -b release-v0.1
    git push
    git push --set-upstream origin release-v0.1

- Add a commit to the release-v0.1 branch to update versions in the code and README to the version. You'll want to do the following search-and-replaces:
    - 's/:latest/:v0.1.0/g' in all files (but not in RELEASING.md!)
    - 's/=latest/=v0.1.0/g' in all files (but not in RELEASING.md!)
- Commit and push that.  CI will make images with the tag `release-v0.1`
- Test the release candidate using those images.
- Once you're happy with the release, tag it and push the tag:
    - git tag v0.1.0
    - git push origin --tags
- CI will make images with the tag `v0.1.0` and `latest`
- Use the github UI to create the release from the tag: https://github.com/projectcalico/tiger-bench/releases/new
