---
description: "Deploy AST Metrics to a whole GitHub organization with one command: it opens a pull request on each repository you select."
---

# Deploy to GitHub Organization

AST Metrics provides a powerful command to automatically deploy AST Metrics to multiple repositories in your GitHub organization with a single command. This feature scans your organization, lets you select which repositories to target, and opens a Pull Request on each selected repository.

## How it works

When you run the deployment command:

1. **Scan**: AST Metrics scans your GitHub organization for eligible repositories
2. **Select**: You choose which repositories to deploy to (or select all)
3. **Open PRs**: A Pull Request is automatically opened on each selected repository
4. **You control the merge**: The PRs are only opened - you remain in control and decide when to merge them

!!! important "You control the merge"

    The Pull Requests are **only opened**, not automatically merged. You or your team members will need to review and merge each PR manually. This gives you full control over when AST Metrics is integrated into each repository.

## Required GitHub Token Permissions

To use this feature, you need a GitHub Personal Access Token with the following permissions:

- **`repo`** (write): Required to create branches and commit workflow files
- **`pull_requests`** (write): Required to open Pull Requests
- **`workflows`** (write): Required to add or modify GitHub Actions workflow files

### Creating a GitHub Token

1. Go to [GitHub Settings > Developer settings > Personal access tokens > Tokens (classic)](https://github.com/settings/tokens)
2. Click "Generate new token" > "Generate new token (classic)"
3. Give your token a descriptive name (e.g., "AST Metrics Deployment")
4. Select the following scopes:
   - ✅ `repo` (Full control of private repositories)
   - ✅ `workflow` (Update GitHub Action workflows)
5. Click "Generate token"
6. Copy the token immediately (you won't be able to see it again)

## Usage

```bash
ast-metrics deploy:github --token=<github-token> <organization-name>
```

### Example

```bash
# Deploy to all or selected repositories in your organization
ast-metrics deploy:github --token=ghp_xxxxxxxxxxxx my-company

# The command will:
# 1. Scan your organization
# 2. Show you a list of eligible repositories
# 3. Let you select which ones to deploy to
# 4. Open a PR on each selected repository
```

### Using Environment Variables

You can also set your GitHub token as an environment variable to avoid passing it in the command:

```bash
export GITHUB_TOKEN="your_personal_access_token"
ast-metrics deploy:github --token=$GITHUB_TOKEN my-company
```

Or provide it inline:

```bash
GITHUB_TOKEN="your_personal_access_token" ast-metrics deploy:github --token=$GITHUB_TOKEN my-company
```

## What gets added to each repository

The Pull Request will add:

- A GitHub Actions workflow file (`.github/workflows/ast-metrics.yml`)
- Configuration to run AST Metrics on your codebase
- Automated quality metrics reporting

## Next Steps

After the PRs are opened:

1. **Review each PR**: Check the proposed workflow configuration
2. **Customize if needed**: Adjust the workflow settings for each repository's specific needs
3. **Merge when ready**: Merge the PRs at your own pace
4. **Monitor results**: Once merged, AST Metrics will start analyzing your code on each commit

## Tips

- Start with a small subset of repositories to test the deployment
- Review the first PR thoroughly to understand what's being added
- You can customize the workflow file in each PR before merging
- Use the interactive selection to exclude repositories that don't need metrics

## See Also

- [GitHub Actions Integration](./github-actions.md)
- [CI Tips](./tips.md)
