import React from 'react';
import { BaseProps } from '../types/BaseProps';
import AbstractSource from './AbstractSource';
export declare const NATIVE_MODULE_NAME = "RNMBXImageSource";
type Position = [number, number];
type Props = BaseProps & {
    /**
     * A string that uniquely identifies the source.
     */
    id: string;
    /**
     * The id refers to en existing source in the style. Does not create a new source.
     */
    existing?: boolean;
    /**
     * An HTTP(S) URL, absolute file URL, or local file URL to the source image.
     * Gifs are currently not supported.
     */
    url?: number | string;
    /**
     * The top left, top right, bottom right, and bottom left coordinates for the image.
     */
    coordinates?: [Position, Position, Position, Position];
    children?: React.ReactElement | React.ReactElement[];
};
type NativeProps = Props;
/**
 * ImageSource is a content source that is used for a georeferenced raster image to be shown on the map.
 * The georeferenced image scales and rotates as the user zooms and rotates the map
 */
declare class ImageSource extends AbstractSource<Props, NativeProps> {
    _getURL(): string | undefined;
    render(): React.JSX.Element | null;
}
export default ImageSource;
//# sourceMappingURL=ImageSource.d.ts.map