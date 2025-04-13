"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXRasterLayerNativeComponent from '../specs/RNMBXRasterLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

class RasterLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXRasterLayerNativeComponent, {
        ref: this.setNativeLayer,
        ...props
      })
    );
  }
}
export default RasterLayer;
//# sourceMappingURL=RasterLayer.js.map