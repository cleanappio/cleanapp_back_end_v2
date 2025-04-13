export type UnsafeMixed<T> = T;
export type OptionalProp<T> = UnsafeMixed<T>;
import { Double, Int32 } from 'react-native/Libraries/Types/CodegenTypes';
export type Point = {
    x: Int32;
    y: Int32;
};
export type NativeCameraStop = {
    centerCoordinate?: string;
    bounds?: string;
    heading?: Double;
    pitch?: Double;
    zoom?: Double;
    paddingLeft?: Double;
    paddingRight?: Double;
    paddingTop?: Double;
    paddingBottom?: Double;
    duration?: Double;
    mode?: string;
} | null;
//# sourceMappingURL=codegenUtils.d.ts.map