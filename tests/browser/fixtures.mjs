export function fixture(count = 4, now = Date.now()) {
  const timestamp = new Date(now).toISOString();
  const repo = {id: 10, name: 'example/project',
    url: 'https://github.com/example/project'};
  const other = {id: 20, name: 'other/project', url: 'https://github.com/other/project'};
  const status = (availability = 'known', complete = true, reason = 'none') => ({
    availability, complete, reason, observed_at: timestamp,
    source: 'repos/example/project'
  });
  const section = (items, state = status()) => ({status: state, items});
  const claim = {
    availability: 'unknown', verification: 'unverified', reason: 'no_supported_reader',
    source: 'none', binding: null
  };
  const core = {completion: claim, pause: claim, execution: claim};
  const identity = (number, repository = repo) => ({
    repository, id: repository.id * 1000 + number, number,
    url: `https://github.com/${repository.name}/issues/${number}`
  });
  const issues = Array.from({length: count}, (_, i) => ({
    identity: identity(i + 1), title: `Issue ${i + 1}`,
    state: i === 0 ? 'closed' : 'open',
    progress: i === 0 ? ['scope', 'shaped', 'delivered'] : ['scope'],
    assignees: [{id: 33, login: 'maintainer'}], core, blocked_by: section([])
  }));
  const edge = (from, to, resolution = status()) => ({
    from, to: {identity: to, resolution}
  });
  if (count >= 4) {
    issues[0].blocked_by.items = [edge(identity(1), identity(2)),
      edge(identity(1), identity(1, other)),
      edge(identity(1), {...identity(1, {...other, id: 0})},
        status('denied', false, 'permission_denied'))];
    issues[1].blocked_by.items = [edge(identity(2), identity(1))];
    issues[2].blocked_by.items = [edge(identity(3), identity(2))];
  }
  if (count > 4) {
    for (let i = 3; i < count; i++) {
      issues[i].blocked_by.items = [edge(identity(i + 1), identity(2))];
    }
  }
  const pull = {...identity(7), id: 20007,
    url: 'https://github.com/example/project/pull/7'};
  return {
    schema_version: 1, started_at: timestamp, finished_at: timestamp,
    expires_at: new Date(now + 300000).toISOString(), repository: section([repo]),
    issues: section(issues), core,
    pull_requests: section([{identity: pull, state: 'closed', merged_at: timestamp,
      head: 'abc123', checks: section([{id: 77, head: 'abc123', name: 'verify',
        status: 'completed', conclusion: 'success',
        url: 'https://github.com/example/project/runs/77'}])}]),
    runs: section([{repository: repo, id: 88, attempt: 1, head: 'abc123',
      status: 'completed', conclusion: 'success', pull_requests: [pull],
      url: 'https://github.com/example/project/actions/runs/88'}])
  };
}
