import type { HostComponent, ViewProps } from 'react-native';
import { Double } from 'react-native/Libraries/Types/CodegenTypes';
import type { Position } from '../types/Position';
import { UnsafeMixed } from './codegenUtils';
type OptionalProp<T> = UnsafeMixed<T>;
export interface NativeProps extends ViewProps {
    coordinate?: OptionalProp<Position>;
    heading?: OptionalProp<Double>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXCustomLocationProviderNativeComponent.d.ts.map