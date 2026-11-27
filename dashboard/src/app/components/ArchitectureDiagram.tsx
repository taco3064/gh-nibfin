import { architecture } from '../lib/architecture'

import { Diagram } from './diagram'

/** The architecture figure of How it works: where Nibfin runs and where its token goes. */
export function ArchitectureDiagram() {
  return (
    <Diagram
      spec={architecture}
      title="Where Nibfin runs"
      description="Your tools, your host and GitHub, with the path of the Secret NIBFIN_GITHUB_TOKEN. The lists below say the same in text."
    />
  )
}
