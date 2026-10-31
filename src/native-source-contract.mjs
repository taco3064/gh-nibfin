export const sourceInstallationIdentity = (
  core,
  { repository, repositoryId, files },
) => core.contentIdentity({ version: 1, repository, repositoryId, files });

export const nativeSourceBody = (identity) =>
  'Install the reviewed project-local Nibfin workflow, Skills and runtime policy. ' +
  'Shared physical Runner capacity remains independent of this repository.\n\n' +
  'Managed installation: ' +
  identity;
