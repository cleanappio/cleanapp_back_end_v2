import type { HostComponent, ViewProps } from 'react-native';
import type { UnsafeMixed } from './codegenUtils';
export interface NativeProps extends ViewProps {
    id: string;
    existing: boolean;
    config: UnsafeMixed<{
        [key: string]: string;
    }>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXStyleImportNativeComponent.d.ts.map