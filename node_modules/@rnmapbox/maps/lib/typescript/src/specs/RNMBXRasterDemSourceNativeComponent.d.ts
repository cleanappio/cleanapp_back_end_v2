import type { HostComponent, ViewProps } from 'react-native';
import { Double } from 'react-native/Libraries/Types/CodegenTypes';
import { UnsafeMixed } from './codegenUtils';
export interface NativeProps extends ViewProps {
    id: UnsafeMixed<string>;
    existing: UnsafeMixed<boolean>;
    url: UnsafeMixed<string>;
    tileUrlTemplates: UnsafeMixed<Array<string>>;
    minZoomLevel: UnsafeMixed<Double>;
    maxZoomLevel: UnsafeMixed<Double>;
    tileSize: UnsafeMixed<Double>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXRasterDemSourceNativeComponent.d.ts.map