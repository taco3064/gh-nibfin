// Only an explicit successful exit establishes child-process success.
export function childExitCode(result) {
  if (result.error || result.signal || result.status === null) return 1;
  if (result.status === 0) return 0;
  return Number.isInteger(result.status) && result.status > 0 ? result.status : 1;
}
