import type { HostComponent, ViewProps } from 'react-native';
import { DirectEventHandler, Double } from 'react-native/Libraries/Types/CodegenTypes';
import type { UnsafeMixed } from './codegenUtils';
type OnMapboxVectorSourcePressEventType = {
    type: string;
    payload: string;
};
export interface NativeProps extends ViewProps {
    id: UnsafeMixed<string>;
    existing: UnsafeMixed<boolean>;
    url: UnsafeMixed<string>;
    tileUrlTemplates: UnsafeMixed<Array<string>>;
    attribution: UnsafeMixed<string>;
    maxZoomLevel: UnsafeMixed<Double>;
    minZoomLevel: UnsafeMixed<Double>;
    tms: UnsafeMixed<boolean>;
    hasPressListener: UnsafeMixed<boolean>;
    hitbox: UnsafeMixed<any>;
    onMapboxVectorSourcePress: DirectEventHandler<OnMapboxVectorSourcePressEventType>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXVectorSourceNativeComponent.d.ts.map