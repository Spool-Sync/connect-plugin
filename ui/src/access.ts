// Whether this person may change what the module keeps.
//
// The core is asked, because the principal's grants are device holdings and
// never carry a resource action. A core that cannot answer leaves the controls
// away; it still decides every write.
import type { ModuleHost } from '@prusa/connect-ui/slots';

export const mayOperate = (host: ModuleHost): Promise<boolean> =>
  host.client
    .checkAccess({
      body: {
        object_type: 'resource_kind',
        object_id: 'sink',
        action: 'operate_resource',
      },
    })
    .then(
      (answer) => answer.allowed,
      () => false,
    );
