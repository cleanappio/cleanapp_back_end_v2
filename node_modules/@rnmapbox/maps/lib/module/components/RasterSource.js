"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import { cloneReactChildrenWithProps } from '../utils';
import RNMBXRasterSourceNativeComponent from '../specs/RNMBXRasterSourceNativeComponent';
import AbstractSource from './AbstractSource';
import { jsx as _jsx } from "react/jsx-runtime";
const MapboxGL = NativeModules.RNMBXModule;
const isTileTemplateUrl = url => !!url && (url.includes('{z}') || url.includes('{bbox-') || url.includes('{quadkey}'));
/**
 * RasterSource is a map content source that supplies raster image tiles to be shown on the map.
 * The location of and metadata about the tiles are defined either by an option dictionary
 * or by an external file that conforms to the TileJSON specification.
 */
class RasterSource extends AbstractSource {
  static defaultProps = {
    id: MapboxGL.StyleSource.DefaultSourceID
  };
  constructor(props) {
    super(props);
    if (isTileTemplateUrl(props.url)) {
      console.warn(`RasterSource 'url' property contains a Tile URL Template, but is intended for a StyleJSON URL. Please migrate your VectorSource to use: \`tileUrlTemplates=["${props.url}"]\` instead.`);
    }
  }
  render() {
    let {
      url
    } = this.props;
    let {
      tileUrlTemplates
    } = this.props;

    // Swapping url for tileUrlTemplates to provide backward compatibility
    // when RasterSource supported only tile url as url prop
    if (isTileTemplateUrl(url)) {
      tileUrlTemplates = [url];
      url = undefined;
    }
    const props = {
      ...this.props,
      id: this.props.id,
      existing: this.props.existing,
      url,
      tileUrlTemplates,
      minZoomLevel: this.props.minZoomLevel,
      maxZoomLevel: this.props.maxZoomLevel,
      tileSize: this.props.tileSize,
      tms: this.props.tms,
      attribution: this.props.attribution
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXRasterSourceNativeComponent, {
        ref: this.setNativeRef,
        ...props,
        children: cloneReactChildrenWithProps(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
export default RasterSource;
//# sourceMappingURL=RasterSource.js.map