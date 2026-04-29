---
name: commit-with-message
description: Create a commit with a message based on the staged changes

---

1. Pre-check

Abort if there is no staged changes
Abort if there are openspec changes that have not been archived (ie, there are sibling folders to `openspec/changes/archive`)

2. Prepare a commit message for the staged changes

- Suggest a commit message based on the **staged only** changes 
- Do not mention the changes in the `/openspec` folder
- Surround folder names, file names, variable names, function and method names by backticks (`) 
- Use the Conventional Commits from https://www.conventionalcommits.org/en/v1.0.0/
- Include the "Assisted-by:" trail with the name of the current model

Show the suggested message and prompt the user to confirm that we shall proceed with committing the staged changes
Abort if the user is not happy with the message

3. Commit 

run the `git commit -s` command with the message prepared above