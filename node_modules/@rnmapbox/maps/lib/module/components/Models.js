"use strict";

import React from 'react';
import { Image } from 'react-native';
import NativeModels from '../specs/RNMBXModelsNativeComponent';
import { jsx as _jsx } from "react/jsx-runtime";
function _resolveAssets(models) {
  const resolvedModels = {};
  Object.keys(models).forEach(key => {
    const model = models[key];
    if (typeof model === 'string') {
      resolvedModels[key] = {
        url: model
      };
    } else {
      const asset = Image.resolveAssetSource(model);
      if (!asset) {
        throw new Error(`Could not resolve model asset: ${model}`);
      }
      resolvedModels[key] = asset;
    }
  });
  return resolvedModels;
}

/**
 * Name of 3D model assets to be used in the map
 */
export default function Models(props) {
  const {
    models,
    ...restOfProps
  } = props;
  return /*#__PURE__*/_jsx(NativeModels, {
    ...restOfProps,
    models: _resolveAssets(models)
  });
}
//# sourceMappingURL=Models.js.map