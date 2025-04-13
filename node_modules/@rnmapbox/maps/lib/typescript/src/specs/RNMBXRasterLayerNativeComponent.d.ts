import type { HostComponent, ViewProps } from 'react-native';
import { Double, Int32 } from 'react-native/Libraries/Types/CodegenTypes';
import { FilterExpression } from '../utils/MapboxStyles';
import type { UnsafeMixed } from './codegenUtils';
type OptionalProp<T> = UnsafeMixed<T>;
type Slot = 'bottom' | 'middle' | 'top';
type CommonProps = {
    sourceID?: OptionalProp<string>;
    existing?: OptionalProp<boolean>;
    filter?: UnsafeMixed<FilterExpression>;
    aboveLayerID?: OptionalProp<string>;
    belowLayerID?: OptionalProp<string>;
    layerIndex?: OptionalProp<Int32>;
    maxZoomLevel?: OptionalProp<Double>;
    minZoomLevel?: OptionalProp<Double>;
    sourceLayerID?: OptionalProp<string>;
    slot?: OptionalProp<Slot>;
};
export interface NativeProps extends ViewProps, CommonProps {
    id?: OptionalProp<string>;
    reactStyle: UnsafeMixed<any>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXRasterLayerNativeComponent.d.ts.map