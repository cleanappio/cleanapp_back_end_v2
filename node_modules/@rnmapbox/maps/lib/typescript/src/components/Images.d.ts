import React, { ReactElement } from 'react';
import { ImageSourcePropType, ImageResolvedAssetSource } from 'react-native';
import Image from './Image';
export declare const NATIVE_MODULE_NAME = "RNMBXImages";
export type RNMBEvent<PayloadType = {
    [key: string]: string;
}> = {
    payload: PayloadType;
    type: string;
};
type TypedReactNode<T> = ReactElement<T> | Array<TypedReactNode<T>> | never;
type NativeImage = string | {
    name: string;
    sdf?: boolean;
    stretchX?: [number, number][];
    stretchY?: [number, number][];
    content?: [number, number, number, number];
    scale?: number;
};
export type ImageEntryData = {
    url?: string;
    image?: ImageSourcePropType;
    resolvedImage?: ImageResolvedAssetSource;
    sdf?: boolean;
    stretchX?: [number, number][];
    stretchY?: [number, number][];
    content?: [number, number, number, number];
    scale?: number;
};
type ResolvedImageEntryData = {
    url?: string;
    resolvedImage?: ImageResolvedAssetSource;
    sdf?: boolean;
    stretchX?: [number, number][];
    stretchY?: [number, number][];
    content?: [number, number, number, number];
    scale?: number;
};
export type ImageEntry = string | ImageSourcePropType | ImageEntryData;
interface Props {
    /**
     * Specifies the external images in key-value pairs required for the shape source.
     * Keys are names - see iconImage expressions, values can be either urls-s objects
     * with format `{uri: 'http://...'}` or `require('image.png')` or `import 'image.png'`
     */
    images?: {
        [key: string]: ImageEntry;
    };
    /**
     * If you have an asset under Image.xcassets on iOS and the drawables directory on android
     * you can specify an array of string names with assets as the key `['pin']`.
     * Additionally object with keys sdf, and strechX, strechY is supported for [SDF icons](https://docs.mapbox.com/help/troubleshooting/using-recolorable-images-in-mapbox-maps/)
     */
    nativeAssetImages?: NativeImage[];
    /**
     * Gets called when a Layer is trying to render an image whose key is not present in
     * any of the `Images` component of the Map.
     */
    onImageMissing?: (imageKey: string) => void;
    children?: TypedReactNode<typeof Image>;
}
/**
 * Images defines the images used in Symbol etc. layers.
 */
declare class Images extends React.PureComponent<Props> {
    static NATIVE_ASSETS_KEY: string;
    _getImages(): {
        images?: undefined;
        nativeImages?: undefined;
    } | {
        images: {
            [key: string]: string | ImageResolvedAssetSource | ResolvedImageEntryData;
        };
        nativeImages: NativeImage[];
    };
    _onImageMissing(event: React.SyntheticEvent<Element, RNMBEvent>): void;
    render(): React.JSX.Element;
}
export default Images;
//# sourceMappingURL=Images.d.ts.map