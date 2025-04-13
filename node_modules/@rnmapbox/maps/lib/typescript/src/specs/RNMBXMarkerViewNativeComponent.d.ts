import type { HostComponent, ViewProps } from 'react-native';
import { Int32 } from 'react-native/Libraries/Types/CodegenTypes';
import { Position } from '../types/Position';
import { UnsafeMixed } from './codegenUtils';
type Point = {
    x: Int32;
    y: Int32;
};
export interface NativeProps extends ViewProps {
    coordinate?: UnsafeMixed<Position>;
    anchor: UnsafeMixed<Point>;
    allowOverlap: UnsafeMixed<boolean>;
    allowOverlapWithPuck: UnsafeMixed<boolean>;
    isSelected: UnsafeMixed<boolean>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXMarkerViewNativeComponent.d.ts.map