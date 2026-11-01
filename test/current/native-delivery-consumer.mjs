import test from 'node:test';
import { withCurrentCore } from '../../src/current-core.mjs';
import { runNativeShapeStages } from './native-shape-stage-fixture.mjs';

test('native Shape, independent Validator and controlled publication owners', async () =>
  withCurrentCore((core, context) => runNativeShapeStages(core, context)));
