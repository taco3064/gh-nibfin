// Inventory is display evidence. The authenticated server authorizes each read.
const get = id => document.getElementById(id);
let openProject;
get('open-project').addEventListener('click', () => {
  const option = get('project-picker').selectedOptions[0];
  if (option && !option.disabled && option.repository) {
    void openProject(option.repository);
  }
});
export function renderProjects(value, readRepository) {
  openProject = readRepository;
  const picker = get('project-picker');
  const status = value.repositories?.status;
  const current = Date.now() >= Date.parse(value.observed_at) &&
    Date.now() < Date.parse(value.expires_at);
  const repositories = value.repositories?.items ?? [];
  const retained = repositories.some(repo => repo.retained);
  const available = status?.availability === 'known';
  get('project-coverage').textContent = !current ?
    'Stale project inventory. Refresh to verify access.' :
    `${available ? 'Observed' : 'Unavailable or denied'} project inventory. ` +
    `${status?.complete ? 'Complete' : 'Partial'} coverage. ` +
    (retained ? 'Retained projects are unavailable until verified again.' :
      !repositories.length && status?.complete ? 'No adopted projects.' :
        !repositories.length ? 'No projects verified; inventory is incomplete.' :
          'Select a project, including those with no assigned open Issues.');
  const existing = new Map([...picker.options].map(option => [option.value, option]));
  const keep = new Set();
  for (const repo of repositories) {
    const key = `${repo.id}:${repo.name}`;
    keep.add(key);
    let option = existing.get(key);
    if (!option) {
      option = document.createElement('option');
      option.value = key;
      picker.append(option);
    }
    option.repository = repo;
    option.disabled = !current || repo.retained || !(repo.id > 0);
    option.textContent = repo.name + (repo.retained ? ' (retained)' :
      !current ? ' (stale)' : '');
  }
  for (const option of [...picker.options]) {
    if (!keep.has(option.value)) option.remove();
  }
  picker.disabled = !repositories.length;
  const update = () => {
    get('open-project').disabled = picker.disabled ||
      !picker.selectedOptions[0] || picker.selectedOptions[0].disabled;
  };
  picker.onchange = update;
  update();
}
