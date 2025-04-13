import type { HostComponent, ViewProps } from 'react-native';
import type { Double } from 'react-native/Libraries/Types/CodegenTypes';
import type { UnsafeMixed } from './codegenUtils';
export interface NativeProps extends ViewProps {
    stretchX: UnsafeMixed<Array<any>>;
    stretchY: UnsafeMixed<Array<any>>;
    content: UnsafeMixed<Array<Double>>;
    sdf: UnsafeMixed<boolean>;
    name: UnsafeMixed<string>;
    scale?: UnsafeMixed<number>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXImageNativeComponent.d.ts.map