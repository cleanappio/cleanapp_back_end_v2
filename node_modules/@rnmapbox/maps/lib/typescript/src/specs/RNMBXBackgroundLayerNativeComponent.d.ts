import type { HostComponent, ViewProps } from 'react-native';
import { Double, Int32 } from 'react-native/Libraries/Types/CodegenTypes';
import type { UnsafeMixed } from './codegenUtils';
type OptionalProp<T> = UnsafeMixed<T>;
export interface NativeProps extends ViewProps {
    id: UnsafeMixed<string>;
    sourceID: UnsafeMixed<string>;
    existing?: OptionalProp<boolean>;
    filter: UnsafeMixed<any[]>;
    aboveLayerID?: OptionalProp<string>;
    belowLayerID?: OptionalProp<string>;
    layerIndex?: OptionalProp<Int32>;
    reactStyle: UnsafeMixed<any>;
    maxZoomLevel?: OptionalProp<Double>;
    minZoomLevel?: OptionalProp<Double>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXBackgroundLayerNativeComponent.d.ts.map