import type { HostComponent, ViewProps } from 'react-native';
import { UnsafeMixed } from './codegenUtils';
export interface NativeProps extends ViewProps {
    id: UnsafeMixed<string>;
    existing: UnsafeMixed<boolean>;
    url: UnsafeMixed<string>;
    coordinates: UnsafeMixed<any>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXImageSourceNativeComponent.d.ts.map