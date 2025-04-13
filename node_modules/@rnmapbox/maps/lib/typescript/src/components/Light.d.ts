import React from 'react';
import { LightLayerStyleProps } from '../utils/MapboxStyles';
import { type BaseProps } from '../types/BaseProps';
interface LightMethods {
    setNativeProps(props: {
        [key: string]: unknown;
    }): void;
}
declare const _default: React.MemoExoticComponent<React.ForwardRefExoticComponent<BaseProps & {
    /**
     * Customizable style attributes
     */
    style: LightLayerStyleProps;
} & React.RefAttributes<LightMethods>>>;
export default _default;
//# sourceMappingURL=Light.d.ts.map