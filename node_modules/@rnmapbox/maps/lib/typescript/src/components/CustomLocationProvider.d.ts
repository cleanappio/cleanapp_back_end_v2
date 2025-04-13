import React from 'react';
import { Position } from '../types/Position';
export type Props = {
    /**
     * longitude and latitude to use for the custom location provider that gets applied to the NativeUserLocation
     */
    coordinate?: Position;
    /**
     * heading/bearing to use for custom location provider that gets applied to the NativeUserLocation
     */
    heading?: number;
};
declare const CustomLocationProvider: React.MemoExoticComponent<(props: Props) => React.JSX.Element>;
export default CustomLocationProvider;
//# sourceMappingURL=CustomLocationProvider.d.ts.map