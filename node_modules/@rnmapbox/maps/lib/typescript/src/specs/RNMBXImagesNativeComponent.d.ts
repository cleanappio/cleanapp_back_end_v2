import type { HostComponent, ViewProps } from 'react-native';
import type { DirectEventHandler } from 'react-native/Libraries/Types/CodegenTypes';
import type { UnsafeMixed } from './codegenUtils';
type OnImageMissingEventType = {
    type: string;
    payload: {
        imageKey: string;
    };
};
export interface NativeProps extends ViewProps {
    images: UnsafeMixed<any>;
    nativeImages: UnsafeMixed<Array<any>>;
    hasOnImageMissing: UnsafeMixed<boolean>;
    onImageMissing: DirectEventHandler<OnImageMissingEventType>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXImagesNativeComponent.d.ts.map