"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXRasterDemSourceNativeComponent from '../specs/RNMBXRasterDemSourceNativeComponent';
import { cloneReactChildrenWithProps } from '../utils';
import AbstractSource from './AbstractSource';
import { jsx as _jsx } from "react/jsx-runtime";
const isTileTemplateUrl = url => !!url && (url.includes('{z}') || url.includes('{bbox-') || url.includes('{quadkey}'));
const MapboxGL = NativeModules.RNMBXModule;
class RasterDemSource extends AbstractSource {
  static defaultProps = {
    id: MapboxGL.StyleSource.DefaultSourceID
  };
  constructor(props) {
    super(props);
    if (isTileTemplateUrl(props.url)) {
      console.warn(`RasterDemSource 'url' property contains a Tile URL Template, but is intended for a StyleJSON URL. Please migrate your VectorSource to use: \`tileUrlTemplates=["${props.url}"]\` instead.`);
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
      tileSize: this.props.tileSize
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXRasterDemSourceNativeComponent, {
        ref: this.setNativeRef,
        ...props,
        children: cloneReactChildrenWithProps(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
export default RasterDemSource;
//# sourceMappingURL=RasterDemSource.js.map