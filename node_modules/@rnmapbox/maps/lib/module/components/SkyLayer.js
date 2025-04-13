"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXSkyLayerNativeComponent from '../specs/RNMBXSkyLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;
/**
 * SkyLayer is a spherical dome around the map that is always rendered behind all other layers
 */
class SkyLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    return /*#__PURE__*/_jsx(RNMBXSkyLayerNativeComponent
    // @ts-expect-error just codegen stuff
    , {
      ref: this.setNativeLayer,
      ...this.baseProps
    });
  }
}
export default SkyLayer;
//# sourceMappingURL=SkyLayer.js.map