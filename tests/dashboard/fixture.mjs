export function fixture(now = Date.now(), count = 150) {
  const stamp = new Date(now - 1000).toISOString();
  const repo = {id: 1, name: 'example/project',
    url: 'https://github.com/example/project'};
  const other = {id: 2, name: 'other/project',
    url: 'https://github.com/other/project'};
  const status = (availability = 'known', complete = true, reason = 'none') =>
    ({availability, complete, reason, observed_at: stamp,
      source: 'repos/example/project'});
  const section = items => ({status: status(), items});
  const claim = {availability: 'unknown', verification: 'unverified',
    reason: 'no_supported_reader', source: 'none', binding: null};
  const core = {completion: claim, pause: claim, execution: claim};
  const identity = (number, repository = repo) => ({repository,
    id: repository.id * 1000 + number, number,
    url: `${repository.url}/issues/${number}`});
  const issues = Array.from({length: count}, (_, n) => ({identity: identity(n + 1),
    title: `Issue ${n + 1}`, state: n === 0 ? 'closed' : 'open',
    progress: n === 0 ? ['scope', 'shaped', 'delivered'] : ['scope'],
    assignees: [{id: 1, login: 'member'}], core, blocked_by: section([])}));
  const edge = (from, to, resolution = status()) =>
    ({from: identity(from), to: {identity: to, resolution}});
  issues[0].blocked_by.items = [edge(1, identity(2)), edge(1, identity(1, other)),
    edge(1, {...identity(3, other), repository: {...other, id: 0}},
      status('denied', false, 'permission_denied'))];
  issues[1].blocked_by.items = [edge(2, identity(1))];
  for (let n = 2; n < count; n++) {
    issues[n].blocked_by.items = [edge(n + 1, identity(2))];
  }
  issues[2].blocked_by = {status: status('unknown', false, 'unavailable'), items: null};
  issues[3].blocked_by.status = status('unknown', false, 'collection_limit');
  issues[4].blocked_by.status = status('denied', false, 'permission_denied');
  issues[0].title = '<img src=x onerror="window.pwned=1"> "\u0000<script>bad</script>';
  issues[0].assignees[0].login = '<svg onload="window.pwned=2">\u001b';
  issues[0].identity.url = 'javascript:window.pwned=3';
  issues[count - 1].identity.url = 'https://github.com.evil/example/project/issues/150';
  const pull = {identity: {...identity(10), url: `${repo.url}/pull/10`},
    state: 'closed', merged_at: stamp, head: 'abc', checks: section([
      {id: 50, name: 'green <script>', status: 'completed', conclusion: 'success',
        head: 'abc', url: `${repo.url}/runs/50`}])};
  return {schema_version: 1, started_at: stamp, finished_at: stamp,
    expires_at: new Date(now + 299000).toISOString(), repository: section([repo]),
    issues: section(issues), pull_requests: section([pull]), runs: section([
      {repository: repo, id: 20, attempt: 1, head: 'abc', status: 'completed',
        conclusion: 'success', url: `${repo.url}/actions/runs/20`,
        pull_requests: [pull.identity]}]), core, additive_field: 'ignored'};
}
